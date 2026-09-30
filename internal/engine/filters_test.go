package engine_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// twoSidedCandidate builds a minimal two-sided CandidateType: one sell order
// at sellPrice, one buy order at buyPrice.
func twoSidedCandidate(typeID int32, buyPrice, sellPrice float64) engine.CandidateType {
	return engine.CandidateType{
		TypeID: typeID,
		SellBook: []engine.Order{
			{TypeID: typeID, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: sellPrice, VolumeRemain: 1},
		},
		BuyBook: []engine.Order{
			{TypeID: typeID, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: buyPrice, Range: "station", VolumeRemain: 1},
		},
	}
}

func TestGrossMarginCeilingKeepsCandidatesAtOrBelowTheCeiling(t *testing.T) {
	// gross margin = (100-20)/100 = 80%, exactly at the ceiling: kept.
	universe := []engine.CandidateType{twoSidedCandidate(1, 20, 100)}

	passed, excluded := engine.GrossMarginCeiling(universe, 0.80)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (80%% margin is at, not above, the ceiling)", passed, excluded)
	}
}

func TestGrossMarginCeilingDropsCandidatesAboveTheCeiling(t *testing.T) {
	// gross margin = (100-10)/100 = 90%, above the 80% ceiling: dropped.
	universe := []engine.CandidateType{twoSidedCandidate(1, 10, 100)}

	passed, excluded := engine.GrossMarginCeiling(universe, 0.80)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (90%% margin exceeds the 80%% ceiling)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestThinBookKeepsCandidatesWithTwoOrdersWithinTheBandOnEachSide(t *testing.T) {
	c := twoSidedCandidate(1, 100, 200)
	// second buy order within 5% of best bid 100 (>= 95); second sell order
	// within 5% of best ask 200 (<= 210).
	c.BuyBook = append(c.BuyBook, engine.Order{TypeID: 1, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 96, Range: "station"})
	c.SellBook = append(c.SellBook, engine.Order{TypeID: 1, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 205})

	passed, excluded := engine.ThinBook([]engine.CandidateType{c}, 2, 0.05)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (2 orders within band on each side)", passed, excluded)
	}
}

func TestThinBookDropsACandidateThinOnTheBuySide(t *testing.T) {
	c := twoSidedCandidate(1, 100, 200)
	// only the best bid itself is within 5% of best bid; no second buy order.
	c.SellBook = append(c.SellBook, engine.Order{TypeID: 1, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 205})

	passed, excluded := engine.ThinBook([]engine.CandidateType{c}, 2, 0.05)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (only one buy order within band)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestThinBookDropsACandidateThinOnTheSellSide(t *testing.T) {
	c := twoSidedCandidate(1, 100, 200)
	c.BuyBook = append(c.BuyBook, engine.Order{TypeID: 1, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 96, Range: "station"})
	// only the best ask itself is within 5% of best ask; no second sell order.

	passed, excluded := engine.ThinBook([]engine.CandidateType{c}, 2, 0.05)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (only one sell order within band)", passed)
	}
	if len(excluded) != 1 {
		t.Fatalf("got excluded=%+v, want one entry", excluded)
	}
}

func TestPricingRuleRecommendsACandidateClearingTargetMargin(t *testing.T) {
	universe := []engine.CandidateType{twoSidedCandidate(11399, 18220, 24080)}

	recs, excluded := engine.PricingRule(universe, 100, 0.018, 0.05025, 0.10)

	if len(excluded) != 0 {
		t.Fatalf("got excluded=%+v, want none", excluded)
	}
	if len(recs) != 1 || recs[0].TypeID != 11399 || recs[0].BuyPrice != 18320 || recs[0].SellPrice != 23980 {
		t.Fatalf("got recs=%+v, want one recommendation for 11399 at buy_price=18320 sell_price=23980", recs)
	}
}

func TestPricingRuleDropsACrossedBook(t *testing.T) {
	// spread 200 == 2*delta: B* would equal S*.
	universe := []engine.CandidateType{twoSidedCandidate(34, 3, 5)}

	recs, excluded := engine.PricingRule(universe, 100, 0.018, 0.05025, 0.10)

	if len(recs) != 0 {
		t.Fatalf("got recs=%+v, want none (crossed book)", recs)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 34 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 34 with a non-empty reason", excluded)
	}
}

func TestPricingRuleDropsBelowTargetMargin(t *testing.T) {
	// net margin at these prices (worked example, price_test.go) is
	// ~15.4% \u2014 below an inflated 50% target.
	universe := []engine.CandidateType{twoSidedCandidate(11399, 18220, 24080)}

	recs, excluded := engine.PricingRule(universe, 100, 0.018, 0.05025, 0.50)

	if len(recs) != 0 {
		t.Fatalf("got recs=%+v, want none (net margin below the 50%% target)", recs)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 11399 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 11399 with a non-empty reason", excluded)
	}
}

func TestFilterBookOnlyAppliesTheFourFiltersInOrderWithOneReasonEach(t *testing.T) {
	params := engine.Params{
		Delta:        100,
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		TargetMargin: 0.10,
		Filters:      engine.FilterThresholds{GrossMarginCeiling: 0.80, ThinBookMinOrders: 2, ThinBookBandPct: 0.05},
	}

	// Morphite: clears every book-only filter \u2014 recommended.
	morphite := twoSidedCandidate(11399, 18220, 24080)
	morphite.BuyBook = append(morphite.BuyBook, engine.Order{TypeID: 11399, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18000, Range: "station"})
	morphite.SellBook = append(morphite.SellBook, engine.Order{TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24500})

	// gross margin 90%: dropped by the ceiling before thin book ever runs,
	// even though its book is also thin (one order per side).
	scam := twoSidedCandidate(2, 10, 100)

	// two-sided, under the ceiling, but thin (one order per side).
	thin := twoSidedCandidate(3, 100, 200)

	// crosses the book at delta=100 (spread 4 < 200).
	crossed := twoSidedCandidate(4, 3, 5)
	crossed.BuyBook = append(crossed.BuyBook, engine.Order{TypeID: 4, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 2.9, Range: "station"})
	crossed.SellBook = append(crossed.SellBook, engine.Order{TypeID: 4, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 5.1})

	universe := []engine.CandidateType{morphite, scam, thin, crossed}

	recs, excluded := engine.FilterBookOnly(universe, params)

	if len(recs) != 1 || recs[0].TypeID != 11399 {
		t.Fatalf("got recs=%+v, want only Morphite (11399)", recs)
	}
	if len(excluded) != 3 {
		t.Fatalf("got %d excluded, want 3: %+v", len(excluded), excluded)
	}
	reasons := map[int32]string{}
	for _, e := range excluded {
		if _, dup := reasons[e.TypeID]; dup {
			t.Fatalf("type %d recorded more than one exclusion reason", e.TypeID)
		}
		reasons[e.TypeID] = e.Reason
	}
	if reasons[2] == "" {
		t.Errorf("want a gross-margin-ceiling reason for type 2")
	}
	if reasons[3] == "" {
		t.Errorf("want a thin-book reason for type 3")
	}
	if reasons[4] == "" {
		t.Errorf("want a crossed-book reason for type 4")
	}
}

func TestThinBookIgnoresOrdersOutsideTheBand(t *testing.T) {
	c := twoSidedCandidate(1, 100, 200)
	// a second buy order well outside the 5% band (< 95): doesn't count.
	c.BuyBook = append(c.BuyBook, engine.Order{TypeID: 1, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 50, Range: "station"})
	c.SellBook = append(c.SellBook, engine.Order{TypeID: 1, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 205})

	passed, excluded := engine.ThinBook([]engine.CandidateType{c}, 2, 0.05)

	if len(passed) != 0 || len(excluded) != 1 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate dropped (the second buy order sits outside the band)", passed, excluded)
	}
}
