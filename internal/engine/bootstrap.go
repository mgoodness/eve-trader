package engine

import (
	"fmt"
	"sort"
	"time"
)

// UntrackedHolding is a type's hangar stock at the trade station that the
// ledger does not already account for (spec §7 Bootstrap). Quantity is the
// live Hangar quantity minus the ledger's held quantity for that type.
type UntrackedHolding struct {
	// TypeID is the EVE inventory type.
	TypeID int32
	// Quantity is the untracked unit count at the trade station.
	Quantity int64
}

// UntrackedHoldings returns the Hangar stock at tradeStationID that the
// ledger does not already represent, one entry per type, sorted by TypeID
// (spec §7 Bootstrap; ADR 0004). It is pure.
//
// The live Hangar assets are the ceiling; the ledger's held quantity per
// type is subtracted. "Held" counts what the ledger claims is physically in
// the hangar: a held-unlisted or reserved-for-sale lot's full QuantityTotal,
// and an open-buy lot's delivered QuantityAvailable. A sold lot holds
// nothing. A type whose assets confirm no more than the ledger already
// accounts for yields no entry — the difference is never negative.
func UntrackedHoldings(lots []Lot, assets []Asset, tradeStationID int64) []UntrackedHolding {
	held := make(map[int32]int64)
	for _, lot := range lots {
		switch lot.Status {
		case LotOpenBuy:
			held[lot.TypeID] += lot.QuantityAvailable
		case LotHeldUnlisted, LotReservedForSale:
			held[lot.TypeID] += lot.QuantityTotal
		case LotSold:
			// Sold stock is gone; it never offsets live hangar assets.
		}
	}

	confirmed := make(map[int32]int64)
	for _, a := range assets {
		if a.LocationType == "station" && a.LocationID == tradeStationID && a.LocationFlag == "Hangar" {
			confirmed[a.TypeID] += a.Quantity
		}
	}

	var untracked []UntrackedHolding
	for typeID, quantity := range confirmed {
		if surplus := quantity - held[typeID]; surplus > 0 {
			untracked = append(untracked, UntrackedHolding{TypeID: typeID, Quantity: surplus})
		}
	}
	sort.Slice(untracked, func(i, j int) bool { return untracked[i].TypeID < untracked[j].TypeID })
	return untracked
}

// SeedLots builds a seeded lot for each confirmed untracked holding (spec §7
// Bootstrap; ADR 0004): held-unlisted immediately, with no acquisition price
// and a lot id of the form "seeded-<typeID>-<unixnano>" unique within a run.
// now is the acquisition time for every lot, passed in so this stays pure.
// The caller appends these to the ledger; they are never created anywhere
// else.
func SeedLots(confirmed []UntrackedHolding, now time.Time) []Lot {
	lots := make([]Lot, 0, len(confirmed))
	for _, holding := range confirmed {
		lots = append(lots, Lot{
			LotID:             fmt.Sprintf("seeded-%d-%d", holding.TypeID, now.UnixNano()),
			TypeID:            holding.TypeID,
			SourceOrderID:     SourceOrderSeeded,
			QuantityTotal:     holding.Quantity,
			QuantityAvailable: holding.Quantity,
			AcquisitionPrice:  nil,
			AcquiredAt:        now,
			Status:            LotHeldUnlisted,
		})
	}
	return lots
}
