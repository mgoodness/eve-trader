package engine_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestUniverseGroupsOrdersByTypeAndBuildsEachTypesEffectiveBooks(t *testing.T) {
	orders := []engine.Order{
		// Morphite: two-sided (a station sell, a covering region buy).
		{OrderID: 1, TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		{OrderID: 2, TypeID: 11399, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18220, Range: "station"},
		// Tritanium: sell-only, no covering buy order.
		{OrderID: 3, TypeID: 34, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 5, Range: "region"},
	}

	universe := engine.Universe(orders, tradeStationID, tradeSystemID, nil)

	if len(universe) != 2 {
		t.Fatalf("got %d candidate types, want 2: %+v", len(universe), universe)
	}
	// Sorted by type_id: Tritanium (34) before Morphite (11399).
	if universe[0].TypeID != 34 || universe[1].TypeID != 11399 {
		t.Fatalf("got type IDs %d, %d, want 34, 11399", universe[0].TypeID, universe[1].TypeID)
	}
	if len(universe[0].SellBook) != 1 || len(universe[0].BuyBook) != 0 {
		t.Errorf("got Tritanium sell=%d buy=%d, want sell=1 buy=0", len(universe[0].SellBook), len(universe[0].BuyBook))
	}
	if len(universe[1].SellBook) != 1 || len(universe[1].BuyBook) != 1 {
		t.Errorf("got Morphite sell=%d buy=%d, want sell=1 buy=1", len(universe[1].SellBook), len(universe[1].BuyBook))
	}
}

func TestTwoSidedKeepsOnlyCandidatesWithACoveringBidAndAStationAsk(t *testing.T) {
	orders := []engine.Order{
		// Morphite: two-sided.
		{OrderID: 1, TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		{OrderID: 2, TypeID: 11399, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18220, Range: "station"},
		// Tritanium: sell-only.
		{OrderID: 3, TypeID: 34, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 5, Range: "region"},
		// Pyerite: buy-only.
		{OrderID: 4, TypeID: 35, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 2, Range: "station"},
	}

	twoSided := engine.TwoSided(engine.Universe(orders, tradeStationID, tradeSystemID, nil))

	if len(twoSided) != 1 {
		t.Fatalf("got %d two-sided types, want 1: %+v", len(twoSided), twoSided)
	}
	if twoSided[0].TypeID != 11399 {
		t.Errorf("got type_id %d, want 11399 (Morphite)", twoSided[0].TypeID)
	}
}
