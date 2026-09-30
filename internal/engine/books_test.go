package engine_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

const (
	tradeStationID int64 = 60004588
	tradeSystemID  int32 = 30002510
)

func TestEffectiveSellBookKeepsOnlyOrdersAtTheTradeStation(t *testing.T) {
	orders := []engine.Order{
		{OrderID: 1, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		{OrderID: 2, IsBuyOrder: false, LocationID: 60004594, SystemID: tradeSystemID, Price: 100, Range: "region"}, // different Rens station
		{OrderID: 3, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18320, Range: "station"},
	}

	book := engine.EffectiveSellBook(orders, tradeStationID)

	if len(book) != 1 {
		t.Fatalf("got %d orders, want 1: %+v", len(book), book)
	}
	if book[0].OrderID != 1 {
		t.Errorf("got order %d, want order 1", book[0].OrderID)
	}
}

func TestEffectiveBuyBookKeepsOrdersWhoseRangeCoversTheTradeStation(t *testing.T) {
	const otherRensStation int64 = 60004594 // a different Rens station, same system
	const otherSystemID int32 = 30002187    // a non-Rens Heimatar system
	const structureID int64 = 1031084757448 // 13-digit Upwell structure ID

	orders := []engine.Order{
		{OrderID: 1, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 100, Range: "station"},
		{OrderID: 2, IsBuyOrder: true, LocationID: otherRensStation, SystemID: tradeSystemID, Price: 200, Range: "solarsystem"},
		{OrderID: 3, IsBuyOrder: true, LocationID: otherRensStation, SystemID: otherSystemID, Price: 300, Range: "region"},
		{OrderID: 4, IsBuyOrder: true, LocationID: otherRensStation, SystemID: otherSystemID, Price: 400, Range: "station"},     // station range, wrong station: doesn't cover
		{OrderID: 5, IsBuyOrder: true, LocationID: otherRensStation, SystemID: otherSystemID, Range: "solarsystem", Price: 500}, // solarsystem range, wrong system: doesn't cover
		{OrderID: 6, IsBuyOrder: true, LocationID: otherRensStation, SystemID: otherSystemID, Range: "5", Price: 600},           // numeric range: deferred to #18, doesn't cover here
		{OrderID: 7, IsBuyOrder: true, LocationID: structureID, SystemID: tradeSystemID, Price: 700, Range: "region"},           // structure, not an NPC station: excluded
		{OrderID: 8, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 800, Range: "station"},      // a sell order: never in the buy book
	}

	book := engine.EffectiveBuyBook(orders, tradeStationID, tradeSystemID)

	var got []int64
	for _, o := range book {
		got = append(got, o.OrderID)
	}
	want := []int64{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got order IDs %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("got order IDs %v, want %v", got, want)
			break
		}
	}
}
