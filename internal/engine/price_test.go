package engine_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// A worked example at the pilot's real fee rates (Trade 4, Broker Relations
// 4, Accounting 3 -> broker 1.8%, sales tax 5.025%, spec §4), against a live
// Morphite book observed at Rens on 2026-09-30: best bid 18220, best ask
// 24080.
func TestPriceComputesFrontOfQueuePricesAndNetMargin(t *testing.T) {
	rec, ok := engine.Price(11399, "Morphite", 18220, 24080, 100, 0.018, 0.05025)
	if !ok {
		t.Fatalf("Price reported not-recommendable for a wide, uncrossed spread")
	}

	want := engine.Recommendation{
		TypeID:        11399,
		Name:          "Morphite",
		BestBid:       18220,
		BestAsk:       24080,
		Spread:        5860,
		BuyPrice:      18320,
		SellPrice:     23980,
		ProfitPerUnit: 3693.605,
		NetMargin:     0.154028565471226,
	}
	if rec.TypeID != want.TypeID || rec.Name != want.Name ||
		rec.BestBid != want.BestBid || rec.BestAsk != want.BestAsk ||
		rec.Spread != want.Spread || rec.BuyPrice != want.BuyPrice ||
		rec.SellPrice != want.SellPrice ||
		math.Abs(rec.ProfitPerUnit-want.ProfitPerUnit) > 1e-6 ||
		math.Abs(rec.NetMargin-want.NetMargin) > 1e-9 {
		t.Errorf("got %+v, want %+v", rec, want)
	}
}

func TestPriceRefusesWhenTheTickWouldCrossTheBook(t *testing.T) {
	// spread 200 == 2*delta: B* would equal S*, so no recommendation.
	_, ok := engine.Price(34, "Tritanium", 3.0, 5.0, 100, 0.018, 0.05025)
	if ok {
		t.Fatalf("Price recommended a candidate whose tick crosses the book")
	}
}
