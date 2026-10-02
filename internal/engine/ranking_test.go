package engine_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestRankSortsDescendingByExpectedDailyProfit(t *testing.T) {
	recs := []engine.BuyRecommendation{
		{TypeID: 1, ProfitPerUnit: 10, AverageDailyVolume: 100},  // EDP 200
		{TypeID: 2, ProfitPerUnit: 100, AverageDailyVolume: 100}, // EDP 2000
		{TypeID: 3, ProfitPerUnit: 50, AverageDailyVolume: 100},  // EDP 1000
	}

	ranked := engine.Rank(recs, 0.20)

	got := []int32{ranked[0].TypeID, ranked[1].TypeID, ranked[2].TypeID}
	want := []int32{2, 3, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got order %v, want descending by EDP: %v", got, want)
		}
	}
}

func TestRankSortIsStableWhenExpectedDailyProfitTies(t *testing.T) {
	// All three share EDP 1000 (10*0.2*500 == 20*0.2*250 == 100*0.2*50);
	// stable sort must preserve their original relative order.
	recs := []engine.BuyRecommendation{
		{TypeID: 1, ProfitPerUnit: 10, AverageDailyVolume: 500},
		{TypeID: 2, ProfitPerUnit: 20, AverageDailyVolume: 250},
		{TypeID: 3, ProfitPerUnit: 100, AverageDailyVolume: 50},
	}

	ranked := engine.Rank(recs, 0.20)

	got := []int32{ranked[0].TypeID, ranked[1].TypeID, ranked[2].TypeID}
	want := []int32{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got order %v, want original order preserved for a tie: %v", got, want)
		}
	}
}

func TestRankComputesExpectedDailyProfitPerOrderSlotAsEDPOverTwoOrders(t *testing.T) {
	// Each candidate costs 2 orders, a buy and a sell (spec \u00a710).
	recs := []engine.BuyRecommendation{
		{TypeID: 1, ProfitPerUnit: 100, AverageDailyVolume: 50},
	}

	ranked := engine.Rank(recs, 0.20)

	want := (100.0 * 0.20 * 50.0) / 2
	if len(ranked) != 1 || math.Abs(ranked[0].ExpectedDailyProfitPerOrderSlot-want) > 1e-9 {
		t.Fatalf("got %+v, want ExpectedDailyProfitPerOrderSlot=%v", ranked, want)
	}
}

func TestRankComputesRoiOnEscrowAsProfitPerUnitOverBuyPrice(t *testing.T) {
	recs := []engine.BuyRecommendation{
		{TypeID: 1, ProfitPerUnit: 3693.605, BuyPrice: 18320},
	}

	ranked := engine.Rank(recs, 0.20)

	want := 3693.605 / 18320.0
	if len(ranked) != 1 || math.Abs(ranked[0].RoiPerDay-want) > 1e-9 {
		t.Fatalf("got %+v, want RoiPerDay=%v (net profit/unit \u00f7 buy price, CONTEXT.md \"ROI on escrow\")", ranked, want)
	}
}

func TestRankComputesExpectedDailyProfitAsProfitPerUnitTimesCaptureRateTimesADV(t *testing.T) {
	recs := []engine.BuyRecommendation{
		{TypeID: 1, ProfitPerUnit: 100, AverageDailyVolume: 50},
	}

	ranked := engine.Rank(recs, 0.20)

	want := 100.0 * 0.20 * 50.0 // 1000
	if len(ranked) != 1 || math.Abs(ranked[0].ExpectedDailyProfit-want) > 1e-9 {
		t.Fatalf("got %+v, want ExpectedDailyProfit=%v", ranked, want)
	}
}
