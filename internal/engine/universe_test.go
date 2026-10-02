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

	universe := engine.Universe(orders, tradeStationID, tradeSystemID, nil, nil)

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

	twoSided := engine.TwoSided(engine.Universe(orders, tradeStationID, tradeSystemID, nil, nil))

	if len(twoSided) != 1 {
		t.Fatalf("got %d two-sided types, want 1: %+v", len(twoSided), twoSided)
	}
	if twoSided[0].TypeID != 11399 {
		t.Errorf("got type_id %d, want 11399 (Morphite)", twoSided[0].TypeID)
	}
}

func TestUniverseExcludesThePilotsOwnOrderFromTheEffectiveBooks(t *testing.T) {
	orders := []engine.Order{
		// Morphite: the only buy order is the pilot's own (order 2) -- once
		// excluded, its buy book is empty, leaving it sell-only.
		{OrderID: 1, TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		{OrderID: 2, TypeID: 11399, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18220, Range: "station"},
		// Tritanium: the pilot's own sell order (order 3) sits alongside a
		// stranger's competing sell (order 4) and a stranger's buy (order 5)
		// -- it stays two-sided, priced only against the competing orders.
		{OrderID: 3, TypeID: 34, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 4, Range: "region"},
		{OrderID: 4, TypeID: 34, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 5, Range: "region"},
		{OrderID: 5, TypeID: 34, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 1, Range: "station"},
	}
	ownOrderIDs := map[int64]bool{2: true, 3: true}

	universe := engine.Universe(orders, tradeStationID, tradeSystemID, nil, ownOrderIDs)

	if len(universe) != 2 {
		t.Fatalf("got %d candidate types, want 2: %+v", len(universe), universe)
	}
	// Sorted by type_id: Tritanium (34) before Morphite (11399).
	tritanium, morphite := universe[0], universe[1]

	if len(morphite.SellBook) != 1 || len(morphite.BuyBook) != 0 {
		t.Fatalf("got Morphite sell=%d buy=%d, want sell=1 buy=0 (its only buy order was the pilot's own)", len(morphite.SellBook), len(morphite.BuyBook))
	}

	if len(tritanium.SellBook) != 1 || tritanium.SellBook[0].OrderID != 4 {
		t.Fatalf("got Tritanium sell book %+v, want only the stranger's order (4), not the pilot's own (3)", tritanium.SellBook)
	}
	if len(tritanium.BuyBook) != 1 || tritanium.BuyBook[0].OrderID != 5 {
		t.Fatalf("got Tritanium buy book %+v, want the stranger's order (5)", tritanium.BuyBook)
	}
}
