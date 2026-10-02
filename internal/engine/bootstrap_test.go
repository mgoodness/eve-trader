package engine_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestUntrackedHoldingsSubtractsLedgerHeldQuantity(t *testing.T) {
	// The hangar holds 100 Tritanium and 5 Morphite; the ledger already
	// accounts for 40 Tritanium. Only the difference is untracked.
	lots := []engine.Lot{heldUnlistedLot("seeded-34", 34, 40)}
	assets := []engine.Asset{
		hangarAsset(34, 100),
		hangarAsset(11399, 5),
	}

	got := engine.UntrackedHoldings(lots, assets, testStationID)
	want := []engine.UntrackedHolding{
		{TypeID: 34, Quantity: 60},
		{TypeID: 11399, Quantity: 5},
	}
	if !slices.Equal(got, want) {
		t.Errorf("UntrackedHoldings() = %+v, want %+v", got, want)
	}
}

func TestUntrackedHoldingsCountsWhatTheLedgerClaimsIsInTheHangar(t *testing.T) {
	lots := []engine.Lot{
		// A reserved-for-sale lot's stock is still physically in the hangar,
		// even though QuantityAvailable is zero.
		{
			LotID: "lot-reserved", TypeID: 34, SourceOrderID: "1",
			QuantityTotal: 30, QuantityAvailable: 0, Status: engine.LotReservedForSale,
		},
		// An open-buy lot has only delivered QuantityAvailable in the hangar.
		{
			LotID: "lot-open", TypeID: 35, SourceOrderID: "2",
			QuantityTotal: 100, QuantityAvailable: 40, Status: engine.LotOpenBuy,
		},
		// A sold lot holds nothing.
		{
			LotID: "lot-sold", TypeID: 36, SourceOrderID: "3",
			QuantityTotal: 50, QuantityAvailable: 0, Status: engine.LotSold,
		},
	}
	assets := []engine.Asset{
		hangarAsset(34, 30),
		hangarAsset(35, 40),
		hangarAsset(36, 50),
	}

	got := engine.UntrackedHoldings(lots, assets, testStationID)
	// Reserved stock (30) offsets its assets; the open buy's delivered 40
	// offsets 35's assets (the 60 undelivered never reach the hangar); the
	// sold lot holds nothing, so 36's 50 is untracked.
	want := []engine.UntrackedHolding{{TypeID: 36, Quantity: 50}}
	if !slices.Equal(got, want) {
		t.Errorf("UntrackedHoldings() = %+v, want %+v", got, want)
	}
}

func TestUntrackedHoldingsIgnoresStockElsewhereAndNeverGoesNegative(t *testing.T) {
	// 70 units in the ledger but only 40 in the hangar: the ledger already
	// claims more than assets confirm, so nothing is untracked (never a
	// negative quantity).
	lots := []engine.Lot{heldUnlistedLot("seeded-34", 34, 70)}
	assets := []engine.Asset{
		hangarAsset(34, 40),
		// Not at the trade station.
		{ItemID: 2, TypeID: 99, Quantity: 10, LocationID: 60000000, LocationType: "station", LocationFlag: "Hangar"},
		// At the trade station but not Hangar (e.g. a ship's cargo hold).
		{ItemID: 3, TypeID: 100, Quantity: 10, LocationID: testStationID, LocationType: "station", LocationFlag: "Cargo"},
	}

	got := engine.UntrackedHoldings(lots, assets, testStationID)
	if len(got) != 0 {
		t.Errorf("UntrackedHoldings() = %+v, want none", got)
	}
}

func TestSeedLotsCreatesHeldUnlistedLotsWithNoAcquisitionPrice(t *testing.T) {
	now := time.Date(2026, 10, 2, 3, 0, 0, 123456789, time.UTC)

	got := engine.SeedLots([]engine.UntrackedHolding{{TypeID: 34, Quantity: 60}, {TypeID: 35, Quantity: 5}}, now)
	if len(got) != 2 {
		t.Fatalf("SeedLots() returned %d lots, want 2", len(got))
	}

	seen := make(map[string]bool, len(got))
	for _, lot := range got {
		if lot.SourceOrderID != engine.SourceOrderSeeded {
			t.Errorf("SourceOrderID = %q, want %q", lot.SourceOrderID, engine.SourceOrderSeeded)
		}
		if lot.AcquisitionPrice != nil {
			t.Errorf("type %d: AcquisitionPrice = %v, want nil (ADR 0004)", lot.TypeID, *lot.AcquisitionPrice)
		}
		if lot.Status != engine.LotHeldUnlisted {
			t.Errorf("type %d: Status = %q, want %q", lot.TypeID, lot.Status, engine.LotHeldUnlisted)
		}
		if lot.QuantityTotal != lot.QuantityAvailable {
			t.Errorf("type %d: total %d != available %d", lot.TypeID, lot.QuantityTotal, lot.QuantityAvailable)
		}
		if !lot.AcquiredAt.Equal(now) {
			t.Errorf("type %d: AcquiredAt = %v, want the caller-supplied %v", lot.TypeID, lot.AcquiredAt, now)
		}
		if want := fmt.Sprintf("seeded-%d-", lot.TypeID); !strings.HasPrefix(lot.LotID, want) {
			t.Errorf("LotID = %q, want prefix %q", lot.LotID, want)
		}
		if seen[lot.LotID] {
			t.Errorf("duplicate LotID %q", lot.LotID)
		}
		seen[lot.LotID] = true
	}
}
