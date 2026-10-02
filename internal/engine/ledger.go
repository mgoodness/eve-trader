package engine

import (
	"fmt"
	"strconv"
	"time"
)

// LotStatus is a trading-stock lot's lifecycle state (spec §7, ADR 0003).
type LotStatus string

// The four lot states (spec §7): a buy order being filled, stock held and
// sellable, stock reserved by a sell order, and stock already sold. Sold
// lots are kept, never deleted.
const (
	LotOpenBuy         LotStatus = "open-buy"
	LotHeldUnlisted    LotStatus = "held-unlisted"
	LotReservedForSale LotStatus = "reserved-for-sale"
	LotSold            LotStatus = "sold"
)

// SourceOrderSeeded is the SourceOrderID of a seeded lot — pre-existing
// hangar stock entered by the explicit bootstrap action (ADR 0004), not
// bought by this tool.
const SourceOrderSeeded = "seeded"

// Lot is one buy order's worth of trading stock (spec §7, ADR 0003).
// Acquisition price only ever varies across lots of the same type, never
// within one, because a buy order fills at its own posted price with no
// slippage.
type Lot struct {
	// LotID is the ledger's stable identifier for this lot.
	LotID string `json:"lot_id"`
	// TypeID is the EVE inventory type this lot holds.
	TypeID int32 `json:"type_id"`
	// SourceOrderID is the buy order's id as a decimal string, or
	// SourceOrderSeeded for a seeded lot.
	SourceOrderID string `json:"source_order_id"`
	// QuantityTotal is the lot's size: the buy order's volume_total, or the
	// seeded quantity. On a terminal partial fill it shrinks to the units
	// that actually arrived.
	QuantityTotal int64 `json:"quantity_total"`
	// QuantityAvailable is the delivered, unreserved unit count — what a
	// sell recommendation may cover. For an open-buy lot it is the units
	// filled so far.
	QuantityAvailable int64 `json:"quantity_available"`
	// AcquisitionPrice is the per-unit buy price, nil for a seeded lot
	// (ADR 0004).
	AcquisitionPrice *float64 `json:"acquisition_price"`
	// AcquiredAt is when the lot was created.
	AcquiredAt time.Time `json:"acquired_at"`
	// Status is the lot's lifecycle state.
	Status LotStatus `json:"status"`
	// LastSeenVolumeRemain is the order's last observed volume_remain — the
	// reconciliation bookkeeping that distinguishes a partial fill from a
	// full fill and records the unfilled remainder on a terminal order.
	LastSeenVolumeRemain int64 `json:"last_seen_volume_remain"`
}

// CharacterOrder is one row of GET /characters/{id}/orders/ or
// /orders/history/ (docs/research/esi-assets-and-orders.md §2.1). State is
// populated only on the history route, where it is "cancelled" or
// "expired" — never "filled" (research §2.3.1).
type CharacterOrder struct {
	OrderID       int64
	TypeID        int32
	RegionID      int32
	LocationID    int64
	Range         string
	IsBuyOrder    bool
	IsCorporation bool
	Price         float64
	VolumeTotal   int64
	VolumeRemain  int64
	MinVolume     int64
	Duration      int
	Issued        time.Time
	Escrow        float64
	State         string
}

// Asset is one row of GET /characters/{id}/assets/ (research
// esi-assets-and-orders.md §1.1). Assets say what and where, never what was
// paid or when.
type Asset struct {
	ItemID       int64
	TypeID       int32
	Quantity     int64
	LocationID   int64
	LocationType string
	LocationFlag string
	IsSingleton  bool
}

// ReconcileNoteKind classifies one reconciliation outcome.
type ReconcileNoteKind string

// The reconciliation outcomes (spec §7, research esi-assets-and-orders.md
// §2.3): a partial fill, a cancelled/expired terminal state, a confirmed
// full fill, an unresolvable disappearance, and an asset drift clamp.
const (
	NotePartialFill ReconcileNoteKind = "partial-fill"
	NoteTerminal    ReconcileNoteKind = "terminal"
	NoteFullFill    ReconcileNoteKind = "full-fill"
	NoteUnknown     ReconcileNoteKind = "unknown-outcome"
	NoteDriftClamp  ReconcileNoteKind = "drift-clamp"
)

// ReconcileNote is one reconciliation outcome, surfaced to the pilot rather
// than recorded silently (spec §7, §11 `Pending`). OrderID is 0 for a note
// that is not tied to a single order (a drift clamp).
type ReconcileNote struct {
	LotID   string
	TypeID  int32
	OrderID int64
	Kind    ReconcileNoteKind
	Detail  string
}

// Reconcile folds fresh ESI snapshots into the stored lots and returns the
// updated lots plus a note per outcome (spec §7). It is pure: no clock, no
// network, no disk — the CLI adapter fetches orders (GET
// /characters/{id}/orders/), history (.../orders/history/), and assets
// (.../assets/), then calls this. The input slice is never mutated.
//
// Outcomes, each an inference over an ESI gap (research §2.3):
//   - a still-listed order whose volume_remain dropped is an authoritative
//     partial fill (`partial-fill`);
//   - a gone order found in history as cancelled/expired is authoritative:
//     its last-seen remainder never arrives (`terminal`);
//   - a gone order absent from history with last-seen volume_remain 0 is a
//     confirmed full fill (`full-fill`);
//   - a gone order absent from history with a non-zero last-seen remainder
//     is an `unknown-outcome`, never guessed either way;
//   - held-unlisted stock is clamped down to live Hangar assets at the
//     trade station (`drift-clamp`); assets showing more is untracked
//     clutter and is never pulled in.
func Reconcile(lots []Lot, orders []CharacterOrder, history []CharacterOrder, assets []Asset, tradeStationID int64) ([]Lot, []ReconcileNote) {
	updated := make([]Lot, len(lots))
	copy(updated, lots)

	// alreadyHeld marks the lots that were held-unlisted before this pass.
	// Only those are ceiling-checked against assets: a lot that this run's
	// authoritative order routes just credited must not be erased by a
	// possibly-stale asset snapshot (the assets cache window is 3,600s, so
	// a fill from minutes ago need not appear yet).
	alreadyHeld := make([]bool, len(lots))
	for i := range lots {
		alreadyHeld[i] = lots[i].Status == LotHeldUnlisted
	}

	activeByOrderID := make(map[int64]CharacterOrder, len(orders))
	for _, o := range orders {
		activeByOrderID[o.OrderID] = o
	}
	historyByOrderID := make(map[int64]CharacterOrder, len(history))
	for _, h := range history {
		historyByOrderID[h.OrderID] = h
	}

	var notes []ReconcileNote
	for i := range updated {
		lot := &updated[i]
		if lot.Status != LotOpenBuy {
			continue
		}
		orderID, err := strconv.ParseInt(lot.SourceOrderID, 10, 64)
		if err != nil {
			// Not an order-backed lot (e.g. seeded); nothing to reconcile
			// against the order routes.
			continue
		}

		note, ok := reconcileOpenBuyLot(lot, orderID, activeByOrderID, historyByOrderID)
		if ok {
			notes = append(notes, note)
		}
	}

	notes = append(notes, clampHeldUnlistedToAssets(updated, alreadyHeld, assets, tradeStationID)...)
	return updated, notes
}

// reconcileOpenBuyLot applies one of the order-outcome branches to an
// open-buy lot, returning the note it produced (ok is false for an
// unchanged order).
func reconcileOpenBuyLot(lot *Lot, orderID int64, active map[int64]CharacterOrder, history map[int64]CharacterOrder) (ReconcileNote, bool) {
	order, present := active[orderID]
	if present {
		if order.VolumeRemain >= lot.LastSeenVolumeRemain {
			// No decrease: nothing to record. A volume_remain that somehow
			// grew is not a fill, so it is left alone rather than trusted.
			return ReconcileNote{}, false
		}

		filled := lot.LastSeenVolumeRemain - order.VolumeRemain
		lot.QuantityAvailable += filled
		lot.LastSeenVolumeRemain = order.VolumeRemain

		if order.VolumeRemain == 0 {
			lot.Status = LotHeldUnlisted
			lot.QuantityAvailable = lot.QuantityTotal
			return ReconcileNote{
				LotID:   lot.LotID,
				TypeID:  lot.TypeID,
				OrderID: orderID,
				Kind:    NoteFullFill,
				Detail:  fmt.Sprintf("order %d fully filled (%d units)", orderID, lot.QuantityTotal),
			}, true
		}
		return ReconcileNote{
			LotID:   lot.LotID,
			TypeID:  lot.TypeID,
			OrderID: orderID,
			Kind:    NotePartialFill,
			Detail:  fmt.Sprintf("order %d: %d units filled, %d remaining", orderID, filled, order.VolumeRemain),
		}, true
	}

	// The order is gone. Order history is the only other primary signal,
	// and it can only ever say cancelled/expired (research §2.3.1).
	if h, found := history[orderID]; found && (h.State == "cancelled" || h.State == "expired") {
		unfilled := lot.LastSeenVolumeRemain
		lot.QuantityTotal -= unfilled
		if lot.QuantityTotal < 0 {
			lot.QuantityTotal = 0
		}
		lot.QuantityAvailable = lot.QuantityTotal
		lot.Status = LotHeldUnlisted
		return ReconcileNote{
			LotID:   lot.LotID,
			TypeID:  lot.TypeID,
			OrderID: orderID,
			Kind:    NoteTerminal,
			Detail:  fmt.Sprintf("order %d %s: %d unfilled units never arrived", orderID, h.State, unfilled),
		}, true
	}

	if lot.LastSeenVolumeRemain == 0 {
		lot.Status = LotHeldUnlisted
		lot.QuantityAvailable = lot.QuantityTotal
		return ReconcileNote{
			LotID:   lot.LotID,
			TypeID:  lot.TypeID,
			OrderID: orderID,
			Kind:    NoteFullFill,
			Detail:  fmt.Sprintf("order %d fully filled (%d units)", orderID, lot.QuantityTotal),
		}, true
	}

	// Gone, absent from history, and still owing stock: ESI cannot resolve
	// this, so neither does the ledger (research §2.3.4).
	return ReconcileNote{
		LotID:   lot.LotID,
		TypeID:  lot.TypeID,
		OrderID: orderID,
		Kind:    NoteUnknown,
		Detail:  fmt.Sprintf("order %d vanished with %d units unaccounted for", orderID, lot.LastSeenVolumeRemain),
	}, true
}

// clampHeldUnlistedToAssets clamps each type's held-unlisted quantity down
// to the Hangar assets at tradeStationID, taking any deficit from the
// oldest lots first (FIFO) and emitting a drift-clamp note per lot touched
// (spec §7 step 3). Assets are a ceiling only: a type holding more than the
// ledger claims is never pulled up. Only lots already held before this pass
// (alreadyHeld) are candidates, so an authoritative fill credited this run
// is never clamped away by a stale asset snapshot.
func clampHeldUnlistedToAssets(lots []Lot, alreadyHeld []bool, assets []Asset, tradeStationID int64) []ReconcileNote {
	held := make(map[int32]int64)
	for i, lot := range lots {
		if alreadyHeld[i] && lot.Status == LotHeldUnlisted {
			held[lot.TypeID] += lot.QuantityAvailable
		}
	}

	confirmed := make(map[int32]int64)
	for _, a := range assets {
		if a.LocationType == "station" && a.LocationID == tradeStationID && a.LocationFlag == "Hangar" {
			confirmed[a.TypeID] += a.Quantity
		}
	}

	deficit := make(map[int32]int64)
	for typeID, expect := range held {
		if confirmed[typeID] < expect {
			deficit[typeID] = expect - confirmed[typeID]
		}
	}

	var notes []ReconcileNote
	for i := range lots {
		lot := &lots[i]
		if !alreadyHeld[i] || lot.Status != LotHeldUnlisted {
			continue
		}
		remaining := deficit[lot.TypeID]
		if remaining <= 0 {
			continue
		}

		clamped := lot.QuantityAvailable
		if clamped > remaining {
			clamped = remaining
		}
		lot.QuantityAvailable -= clamped
		deficit[lot.TypeID] -= clamped

		notes = append(notes, ReconcileNote{
			LotID:  lot.LotID,
			TypeID: lot.TypeID,
			Kind:   NoteDriftClamp,
			Detail: fmt.Sprintf("type %d: assets confirm %d of %d held units (clamped by %d)",
				lot.TypeID, confirmed[lot.TypeID], held[lot.TypeID], clamped),
		})
	}
	return notes
}
