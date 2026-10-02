package engine_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// ledgerTime is a fixed acquisition timestamp, so lot fixtures never read
// the clock.
var ledgerTime = time.Date(2026, 9, 30, 11, 53, 0, 0, time.UTC)

func pricePtr(v float64) *float64 { return &v }

// openBuyLot builds an open-buy lot for orderID whose filled quantity is
// total-lastSeenRemain (the units already delivered) and whose last-seen
// remainder is lastSeenRemain.
func openBuyLot(lotID string, orderID int64, typeID int32, total, lastSeenRemain int64) engine.Lot {
	return engine.Lot{
		LotID:                lotID,
		TypeID:               typeID,
		SourceOrderID:        strconv.FormatInt(orderID, 10),
		QuantityTotal:        total,
		QuantityAvailable:    total - lastSeenRemain,
		AcquisitionPrice:     pricePtr(100),
		AcquiredAt:           ledgerTime,
		Status:               engine.LotOpenBuy,
		LastSeenVolumeRemain: lastSeenRemain,
	}
}

// heldUnlistedLot builds a held-unlisted lot with the full quantity
// available (the state after a completed buy).
func heldUnlistedLot(lotID string, typeID int32, qty int64) engine.Lot {
	return engine.Lot{
		LotID:             lotID,
		TypeID:            typeID,
		SourceOrderID:     "seeded",
		QuantityTotal:     qty,
		QuantityAvailable: qty,
		AcquisitionPrice:  nil,
		AcquiredAt:        ledgerTime,
		Status:            engine.LotHeldUnlisted,
	}
}

// hangarAsset builds one Hangar asset row at the trade station.
func hangarAsset(typeID int32, qty int64) engine.Asset {
	return engine.Asset{
		ItemID:       1,
		TypeID:       typeID,
		Quantity:     qty,
		LocationID:   60004588,
		LocationType: "station",
		LocationFlag: "Hangar",
	}
}

const testStationID int64 = 60004588

func TestReconcileLeavesAnUnchangedOpenBuyLotAlone(t *testing.T) {
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 100)}
	orders := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, VolumeTotal: 100, VolumeRemain: 100}}

	got, notes := engine.Reconcile(lots, orders, nil, nil, testStationID)

	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none for an order that has not changed", notes)
	}
	if len(got) != 1 || got[0].QuantityAvailable != 0 || got[0].Status != engine.LotOpenBuy || got[0].LastSeenVolumeRemain != 100 {
		t.Fatalf("got %+v, want the open-buy lot unchanged", got)
	}
}

func TestReconcileRecordsAPartialFill(t *testing.T) {
	// An order placed for 100 units has 40 filled since the last poll, so
	// volume_remain dropped from 100 to 60: 40 units are now delivered
	// (spec §7 step 1, research §2.3.3).
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 100)}
	orders := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, VolumeTotal: 100, VolumeRemain: 60}}

	got, notes := engine.Reconcile(lots, orders, nil, nil, testStationID)

	if len(got) != 1 {
		t.Fatalf("got %d lots, want 1", len(got))
	}
	if got[0].Status != engine.LotOpenBuy {
		t.Errorf("status = %q, want %q (the order is still open)", got[0].Status, engine.LotOpenBuy)
	}
	if got[0].QuantityAvailable != 40 {
		t.Errorf("quantity_available = %d, want 40 (the filled units)", got[0].QuantityAvailable)
	}
	if got[0].LastSeenVolumeRemain != 60 {
		t.Errorf("last_seen_volume_remain = %d, want 60", got[0].LastSeenVolumeRemain)
	}
	assertNote(t, notes, engine.NotePartialFill, "lot-1", 1001)
}

func TestReconcileConfirmsAFullFillWhenRemainReachesZeroWhileListed(t *testing.T) {
	// The order is still listed but volume_remain hit 0: a direct full-fill
	// signal (research §2.3.2).
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 10)}
	orders := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, VolumeTotal: 100, VolumeRemain: 0}}

	got, notes := engine.Reconcile(lots, orders, nil, nil, testStationID)

	if got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("status = %q, want %q (fully filled)", got[0].Status, engine.LotHeldUnlisted)
	}
	if got[0].QuantityAvailable != 100 || got[0].QuantityTotal != 100 {
		t.Errorf("quantities = %d/%d, want 100/100", got[0].QuantityAvailable, got[0].QuantityTotal)
	}
	assertNote(t, notes, engine.NoteFullFill, "lot-1", 1001)
}

func TestReconcileConfirmsAFullFillWhenTheOrderDisappearsWithNoRemainder(t *testing.T) {
	// The order is gone, is not in order history, and its last-seen
	// volume_remain was 0: a confirmed full fill (spec §7 step 2).
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 100)}
	lots[0].QuantityAvailable = 100
	lots[0].LastSeenVolumeRemain = 0

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)

	if got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("status = %q, want %q (confirmed full fill)", got[0].Status, engine.LotHeldUnlisted)
	}
	if got[0].QuantityAvailable != 100 {
		t.Errorf("quantity_available = %d, want 100", got[0].QuantityAvailable)
	}
	assertNote(t, notes, engine.NoteFullFill, "lot-1", 1001)
}

func TestReconcileClassifiesATerminalCancellation(t *testing.T) {
	// The order disappeared and order history records "cancelled": the
	// last-seen remainder (40) never arrives, so only the 60 filled units
	// remain (spec §7 step 2).
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 40)}
	history := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, State: "cancelled"}}

	got, notes := engine.Reconcile(lots, nil, history, nil, testStationID)

	if got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("status = %q, want %q (terminal, filled remainder kept)", got[0].Status, engine.LotHeldUnlisted)
	}
	if got[0].QuantityTotal != 60 || got[0].QuantityAvailable != 60 {
		t.Errorf("quantities = %d/%d, want 60/60 (the unfilled 40 never arrives)", got[0].QuantityAvailable, got[0].QuantityTotal)
	}
	assertNote(t, notes, engine.NoteTerminal, "lot-1", 1001)
}

func TestReconcileClassifiesATerminalExpiry(t *testing.T) {
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 40)}
	history := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, State: "expired"}}

	got, notes := engine.Reconcile(lots, nil, history, nil, testStationID)

	if got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("status = %q, want %q (terminal, filled remainder kept)", got[0].Status, engine.LotHeldUnlisted)
	}
	if got[0].QuantityAvailable != 60 {
		t.Errorf("quantity_available = %d, want 60", got[0].QuantityAvailable)
	}
	assertNote(t, notes, engine.NoteTerminal, "lot-1", 1001)
}

func TestReconcileSurfacesAnUnknownOutcome(t *testing.T) {
	// The order disappeared, is absent from history, and last-seen
	// volume_remain was 40: ESI cannot say filled vs cancelled, so the lot
	// must not be guessed either way (spec §7 step 2, research §2.3.4).
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 40)}

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)

	if got[0].Status != engine.LotOpenBuy {
		t.Errorf("status = %q, want %q (unresolved, not guessed)", got[0].Status, engine.LotOpenBuy)
	}
	if got[0].QuantityAvailable != 60 || got[0].LastSeenVolumeRemain != 40 {
		t.Errorf("lot = %+v, want the filled 60 and last-seen 40 preserved", got[0])
	}
	assertNote(t, notes, engine.NoteUnknown, "lot-1", 1001)
}

func TestReconcileClampsHeldUnlistedDownToLiveAssets(t *testing.T) {
	// The ledger claims 100 held units; the hangar only confirms 70. The
	// ledger is clamped down, with a visible warning (spec §7 step 3).
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}
	assets := []engine.Asset{hangarAsset(34, 70)}

	got, notes := engine.Reconcile(lots, nil, nil, assets, testStationID)

	if got[0].QuantityAvailable != 70 {
		t.Errorf("quantity_available = %d, want 70 (clamped to assets)", got[0].QuantityAvailable)
	}
	if got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("status = %q, want %q (still held)", got[0].Status, engine.LotHeldUnlisted)
	}
	assertNote(t, notes, engine.NoteDriftClamp, "lot-1", 0)
}

func TestReconcileNeverPullsTheLedgerUpToAssetsShowingMore(t *testing.T) {
	// Assets showing more than the ledger expects is untracked clutter and
	// is never absorbed (spec §7 step 3, ADR 0003).
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}
	assets := []engine.Asset{hangarAsset(34, 120)}

	got, notes := engine.Reconcile(lots, nil, nil, assets, testStationID)

	if got[0].QuantityAvailable != 100 {
		t.Errorf("quantity_available = %d, want 100 (never pulled up)", got[0].QuantityAvailable)
	}
	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none when assets exceed the ledger", notes)
	}
}

func TestReconcileDoesNotClampAJustFilledLotToAbsentAssets(t *testing.T) {
	// A fill confirmed by the order route this run is authoritative; the
	// asset clamp must not erase it using a possibly-stale assets snapshot
	// (assets cache for 3,600s, so a fill from minutes ago may not be
	// listed yet). Only lots already held before the run are clamp
	// candidates.
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 0)}
	lots[0].QuantityAvailable = 100

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)

	if got[0].QuantityAvailable != 100 {
		t.Errorf("quantity_available = %d, want 100 (a fresh fill is never clamped away)", got[0].QuantityAvailable)
	}
	for _, n := range notes {
		if n.Kind == engine.NoteDriftClamp {
			t.Errorf("got a drift-clamp note %+v for a lot filled this run", n)
		}
	}
}

func TestReconcileMakesNoChangeWhenAssetsMatchTheLedger(t *testing.T) {
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}
	assets := []engine.Asset{hangarAsset(34, 100)}

	got, notes := engine.Reconcile(lots, nil, nil, assets, testStationID)

	if got[0].QuantityAvailable != 100 {
		t.Errorf("quantity_available = %d, want 100", got[0].QuantityAvailable)
	}
	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none with no drift", notes)
	}
}

func TestReconcileClampsAgainstAllHeldLotsOfAType(t *testing.T) {
	// Two held lots of the same type total 150; assets confirm 120, so the
	// 30-unit deficit is taken from the oldest lot first (FIFO: sales
	// consume the oldest stock first).
	lots := []engine.Lot{
		heldUnlistedLot("lot-old", 34, 100),
		heldUnlistedLot("lot-new", 34, 50),
	}
	assets := []engine.Asset{hangarAsset(34, 120)}

	got, _ := engine.Reconcile(lots, nil, nil, assets, testStationID)

	if got[0].QuantityAvailable != 70 {
		t.Errorf("oldest lot quantity_available = %d, want 70", got[0].QuantityAvailable)
	}
	if got[1].QuantityAvailable != 50 {
		t.Errorf("newest lot quantity_available = %d, want 50 (untouched)", got[1].QuantityAvailable)
	}
}

func TestReconcileIgnoresAssetsOutsideTheTradeStationHangar(t *testing.T) {
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}
	assets := []engine.Asset{
		{ItemID: 1, TypeID: 34, Quantity: 40, LocationID: 60004588, LocationType: "station", LocationFlag: "Cargo"},
		{ItemID: 2, TypeID: 34, Quantity: 40, LocationID: 60004589, LocationType: "station", LocationFlag: "Hangar"},
		{ItemID: 3, TypeID: 34, Quantity: 40, LocationID: 60004588, LocationType: "item", LocationFlag: "Hangar"},
	}

	got, notes := engine.Reconcile(lots, nil, nil, assets, testStationID)

	// None of the three rows count as Hangar stock at the trade station:
	// wrong flag, wrong station, wrong location type. The ledger is clamped
	// to zero rather than trusting a non-matching row.
	if got[0].QuantityAvailable != 0 {
		t.Errorf("quantity_available = %d, want 0 (no matching Hangar asset)", got[0].QuantityAvailable)
	}
	assertNote(t, notes, engine.NoteDriftClamp, "lot-1", 0)
}

func TestReconcileSkipsSoldLots(t *testing.T) {
	sold := heldUnlistedLot("lot-1", 34, 100)
	sold.Status = engine.LotSold
	lots := []engine.Lot{sold}

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)

	if got[0].Status != engine.LotSold || got[0].QuantityAvailable != 100 {
		t.Errorf("got %+v, want the sold lot untouched", got[0])
	}
	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none for a sold lot", notes)
	}
}

func TestReconcileDoesNotMutateItsInput(t *testing.T) {
	lots := []engine.Lot{openBuyLot("lot-1", 1001, 34, 100, 100)}
	orders := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, VolumeTotal: 100, VolumeRemain: 60}}

	engine.Reconcile(lots, orders, nil, nil, testStationID)

	if lots[0].QuantityAvailable != 0 || lots[0].LastSeenVolumeRemain != 100 {
		t.Errorf("input lot was mutated: %+v", lots[0])
	}
}

// assertNote requires exactly one note of kind for lotID/orderID.
func assertNote(t *testing.T, notes []engine.ReconcileNote, kind engine.ReconcileNoteKind, lotID string, orderID int64) {
	t.Helper()
	var matching []engine.ReconcileNote
	for _, n := range notes {
		if n.Kind == kind && n.LotID == lotID && n.OrderID == orderID {
			matching = append(matching, n)
		}
	}
	if len(matching) != 1 {
		t.Fatalf("got notes %+v, want exactly one %q for %s/order %d", notes, kind, lotID, orderID)
	}
}

// activeBuyOrder builds one active character buy order at the trade station.
func activeBuyOrder(orderID int64, typeID int32, total, remain int64) engine.CharacterOrder {
	return engine.CharacterOrder{
		OrderID:      orderID,
		TypeID:       typeID,
		LocationID:   testStationID,
		IsBuyOrder:   true,
		Price:        12.5,
		VolumeTotal:  total,
		VolumeRemain: remain,
	}
}

// activeSellOrder builds one active character sell order at the trade station.
func activeSellOrder(orderID int64, typeID int32, remain int64) engine.CharacterOrder {
	return engine.CharacterOrder{
		OrderID:      orderID,
		TypeID:       typeID,
		LocationID:   testStationID,
		IsBuyOrder:   false,
		Price:        20,
		VolumeRemain: remain,
	}
}

// reservedForSaleLot builds a lot completely reserved by an open sell order.
func reservedForSaleLot(lotID string, typeID int32, qty, orderID, lastSeenRemain int64) engine.Lot {
	return engine.Lot{
		LotID:                lotID,
		TypeID:               typeID,
		SourceOrderID:        "seeded",
		QuantityTotal:        qty,
		QuantityAvailable:    0,
		AcquiredAt:           ledgerTime,
		Status:               engine.LotReservedForSale,
		LastSeenVolumeRemain: lastSeenRemain,
		ReservedOrderID:      orderID,
	}
}

// TestReconcileCreatesAnOpenBuyLotForANewActiveBuyOrder proves the ledger
// sees an order placed since the last run (spec §7: one lot per buy order
// that has delivered or is delivering stock). Without it awaiting-buy-fill
// is unreachable and a later fill can never be detected.
func TestReconcileCreatesAnOpenBuyLotForANewActiveBuyOrder(t *testing.T) {
	issued := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	order := activeBuyOrder(1001, 34, 100, 40)
	order.Issued = issued
	order.Price = 12.5

	got, notes := engine.Reconcile(nil, []engine.CharacterOrder{order}, nil, nil, testStationID)

	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none for a newly ingested order", notes)
	}
	if len(got) != 1 {
		t.Fatalf("got %d lots, want 1 created from the active buy order", len(got))
	}
	lot := got[0]
	if lot.LotID != "order-1001" || lot.SourceOrderID != "1001" {
		t.Errorf("got lot id/source %q/%q, want order-1001/1001", lot.LotID, lot.SourceOrderID)
	}
	if lot.TypeID != 34 || lot.QuantityTotal != 100 || lot.QuantityAvailable != 60 {
		t.Errorf("got type/total/available %d/%d/%d, want 34/100/60 (delivered so far)", lot.TypeID, lot.QuantityTotal, lot.QuantityAvailable)
	}
	if lot.Status != engine.LotOpenBuy || lot.LastSeenVolumeRemain != 40 {
		t.Errorf("got status/last-seen %q/%d, want open-buy/40", lot.Status, lot.LastSeenVolumeRemain)
	}
	if lot.AcquisitionPrice == nil || *lot.AcquisitionPrice != 12.5 {
		t.Errorf("got acquisition price %v, want 12.5", lot.AcquisitionPrice)
	}
	if !lot.AcquiredAt.Equal(issued) {
		t.Errorf("got acquired_at %v, want the order's issued time %v", lot.AcquiredAt, issued)
	}
}

// TestReconcileIgnoresCorporationAndOffStationBuyOrders proves the ingestion
// only claims the pilot's own station trading stock (spec §7): corporation
// orders and orders at other locations become no lot.
func TestReconcileIgnoresCorporationAndOffStationBuyOrders(t *testing.T) {
	corp := activeBuyOrder(1, 34, 100, 50)
	corp.IsCorporation = true
	offStation := activeBuyOrder(2, 34, 100, 50)
	offStation.LocationID = 60004589
	mine := activeBuyOrder(3, 34, 100, 50)
	sellNoStock := activeSellOrder(4, 34, 50)

	got, _ := engine.Reconcile(nil, []engine.CharacterOrder{corp, offStation, mine, sellNoStock}, nil, nil, testStationID)

	if len(got) != 1 || got[0].SourceOrderID != "3" {
		t.Fatalf("got %+v, want exactly the pilot's own trade-station buy order", got)
	}
}

// TestReconcileDetectsAPartialFillOfAnIngestedOrderAcrossRuns proves the
// fill-detection algorithm fires on the run after an order is first seen:
// the volume_remain drop is an authoritative partial fill (spec §7 step 1).
func TestReconcileDetectsAPartialFillOfAnIngestedOrderAcrossRuns(t *testing.T) {
	first, _ := engine.Reconcile(nil, []engine.CharacterOrder{activeBuyOrder(1001, 34, 100, 100)}, nil, nil, testStationID)

	got, notes := engine.Reconcile(first, []engine.CharacterOrder{activeBuyOrder(1001, 34, 100, 60)}, nil, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotOpenBuy {
		t.Fatalf("got %+v, want the still-open lot", got)
	}
	if got[0].QuantityAvailable != 40 || got[0].LastSeenVolumeRemain != 60 {
		t.Errorf("got available/last-seen %d/%d, want 40/60", got[0].QuantityAvailable, got[0].LastSeenVolumeRemain)
	}
	assertNote(t, notes, engine.NotePartialFill, "order-1001", 1001)
}

// TestReconcileConfirmsAFullFillOfAnIngestedOrderThatVanishes proves a
// first-seen-then-gone order with no last-seen remainder is a confirmed full
// fill (spec §7 step 2).
func TestReconcileConfirmsAFullFillOfAnIngestedOrderThatVanishes(t *testing.T) {
	first, _ := engine.Reconcile(nil, []engine.CharacterOrder{activeBuyOrder(1001, 34, 100, 0)}, nil, nil, testStationID)

	got, notes := engine.Reconcile(first, nil, nil, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotHeldUnlisted {
		t.Fatalf("got %+v, want the lot held-unlisted after the confirmed fill", got)
	}
	if got[0].QuantityAvailable != 100 || got[0].QuantityTotal != 100 {
		t.Errorf("got available/total %d/%d, want 100/100", got[0].QuantityAvailable, got[0].QuantityTotal)
	}
	assertNote(t, notes, engine.NoteFullFill, "order-1001", 1001)
}

// TestReconcileClassifiesATerminalCancellationOfAnIngestedOrder proves a
// first-seen order that vanishes into history as cancelled keeps only the
// units that actually arrived (spec §7 step 2).
func TestReconcileClassifiesATerminalCancellationOfAnIngestedOrder(t *testing.T) {
	first, _ := engine.Reconcile(nil, []engine.CharacterOrder{activeBuyOrder(1001, 34, 100, 40)}, nil, nil, testStationID)
	history := []engine.CharacterOrder{{OrderID: 1001, TypeID: 34, State: "cancelled"}}

	got, notes := engine.Reconcile(first, nil, history, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotHeldUnlisted {
		t.Fatalf("got %+v, want the terminal lot held-unlisted", got)
	}
	if got[0].QuantityTotal != 60 || got[0].QuantityAvailable != 60 {
		t.Errorf("got total/available %d/%d, want 60/60 (the unfilled 40 never arrives)", got[0].QuantityTotal, got[0].QuantityAvailable)
	}
	assertNote(t, notes, engine.NoteTerminal, "order-1001", 1001)
}

// TestReconcileSurfacesAnUnknownOutcomeOfAnIngestedOrder proves a gone order
// absent from history with stock still owing is never guessed (spec §7 step
// 2; research §2.3.4).
func TestReconcileSurfacesAnUnknownOutcomeOfAnIngestedOrder(t *testing.T) {
	first, _ := engine.Reconcile(nil, []engine.CharacterOrder{activeBuyOrder(1001, 34, 100, 40)}, nil, nil, testStationID)

	got, notes := engine.Reconcile(first, nil, nil, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotOpenBuy {
		t.Fatalf("got %+v, want the unresolved lot left open-buy", got)
	}
	if got[0].QuantityAvailable != 60 || got[0].LastSeenVolumeRemain != 40 {
		t.Errorf("got available/last-seen %d/%d, want 60/40", got[0].QuantityAvailable, got[0].LastSeenVolumeRemain)
	}
	assertNote(t, notes, engine.NoteUnknown, "order-1001", 1001)
}

// TestReconcileReservesHeldStockCoveredByAnOpenSellOrder proves an open
// station sell order reserves held stock and persists it (spec §7, §11).
func TestReconcileReservesHeldStockCoveredByAnOpenSellOrder(t *testing.T) {
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}

	got, notes := engine.Reconcile(lots, []engine.CharacterOrder{activeSellOrder(5000, 34, 100)}, nil, nil, testStationID)

	if len(notes) != 0 {
		t.Errorf("got notes %+v, want none for a fresh reservation", notes)
	}
	if len(got) != 1 || got[0].Status != engine.LotReservedForSale {
		t.Fatalf("got %+v, want the lot reserved-for-sale", got)
	}
	if got[0].QuantityAvailable != 0 || got[0].ReservedOrderID != 5000 {
		t.Errorf("got available/reserved-order %d/%d, want 0/5000", got[0].QuantityAvailable, got[0].ReservedOrderID)
	}
}

// TestReconcileReservesOnlyTheUnitsAnOpenOrderCovers proves a partly covered
// lot keeps its held-unlisted status with only its reserved units removed, so
// the unreserved remainder stays recommendable.
func TestReconcileReservesOnlyTheUnitsAnOpenOrderCovers(t *testing.T) {
	lots := []engine.Lot{heldUnlistedLot("lot-1", 34, 100)}

	got, _ := engine.Reconcile(lots, []engine.CharacterOrder{activeSellOrder(5000, 34, 60)}, nil, []engine.Asset{hangarAsset(34, 100)}, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotHeldUnlisted {
		t.Fatalf("got %+v, want the lot held-unlisted with a partial reservation", got)
	}
	if got[0].QuantityAvailable != 40 || got[0].ReservedOrderID != 5000 {
		t.Errorf("got available/reserved-order %d/%d, want 40/5000", got[0].QuantityAvailable, got[0].ReservedOrderID)
	}
}

// TestReconcileReleasesAReservationWhenTheSellOrderIsCancelled proves a
// cancelled sell order releases its reservation rather than losing the stock
// (spec §7).
func TestReconcileReleasesAReservationWhenTheSellOrderIsCancelled(t *testing.T) {
	lots := []engine.Lot{reservedForSaleLot("lot-1", 34, 100, 5000, 100)}
	history := []engine.CharacterOrder{{OrderID: 5000, TypeID: 34, State: "cancelled"}}

	got, notes := engine.Reconcile(lots, nil, history, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotHeldUnlisted {
		t.Fatalf("got %+v, want the released lot held-unlisted", got)
	}
	if got[0].QuantityAvailable != 100 || got[0].ReservedOrderID != 0 {
		t.Errorf("got available/reserved-order %d/%d, want 100/0", got[0].QuantityAvailable, got[0].ReservedOrderID)
	}
	assertNote(t, notes, engine.NoteTerminal, "lot-1", 5000)
}

// TestReconcileFinalizesAFilledSellOrderToSoldAndKeepsItSold proves a filled
// sell order's stock is sold and kept sold across another run, never
// resurrected as held-unlisted (spec §7).
func TestReconcileFinalizesAFilledSellOrderToSoldAndKeepsItSold(t *testing.T) {
	lots := []engine.Lot{reservedForSaleLot("lot-1", 34, 100, 5000, 0)}

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)
	if len(got) != 1 || got[0].Status != engine.LotSold {
		t.Fatalf("got %+v, want the lot sold", got)
	}
	if got[0].QuantityAvailable != 0 {
		t.Errorf("got available %d, want 0 for a sold lot", got[0].QuantityAvailable)
	}
	assertNote(t, notes, engine.NoteSold, "lot-1", 5000)

	// Another run with no fresh orders must leave the sold lot alone.
	again, againNotes := engine.Reconcile(got, nil, nil, nil, testStationID)
	if len(again) != 1 || again[0].Status != engine.LotSold {
		t.Fatalf("got %+v, want the sold lot to stay sold", again)
	}
	if len(againNotes) != 0 {
		t.Errorf("got notes %+v, want none for an already-sold lot", againNotes)
	}
}

// TestReconcileLeavesAnUnknownSellOutcomeUnresolved proves a reservation
// whose order vanished with stock still owing is kept reserved and surfaced,
// never guessed filled or cancelled (spec §7 step 2).
func TestReconcileLeavesAnUnknownSellOutcomeUnresolved(t *testing.T) {
	lots := []engine.Lot{reservedForSaleLot("lot-1", 34, 100, 5000, 40)}

	got, notes := engine.Reconcile(lots, nil, nil, nil, testStationID)

	if len(got) != 1 || got[0].Status != engine.LotReservedForSale || got[0].ReservedOrderID != 5000 {
		t.Fatalf("got %+v, want the unknown reservation left reserved-for-sale", got)
	}
	assertNote(t, notes, engine.NoteUnknown, "lot-1", 5000)
}
