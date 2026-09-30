package engine_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestCapitalNeededReportsWhatACandidateWouldNeedToBeFundedAtItsUnitsCap(t *testing.T) {
	rec := engine.Recommendation{BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100}

	// units cap = 0.20 * 100 * 3 = 60; capital/unit = 1000*1.018 + 1100*0.018 = 1037.8
	want := 1037.8 * 60
	got := engine.CapitalNeeded(rec, 0.018, 0.20, 3)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("got CapitalNeeded=%v, want %v", got, want)
	}
}

func TestAllocateFundsACandidateAtItsUnitsCapWhenBudgetComfortablyCoversIt(t *testing.T) {
	ranked := []engine.Recommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, ProfitPerUnit: 90, AverageDailyVolume: 100},
	}
	params := engine.AllocationParams{
		Budget:      150_000,
		OrderLimit:  21,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, unfunded := engine.Allocate(ranked, params)

	if len(unfunded) != 0 {
		t.Fatalf("got unfunded=%+v, want none", unfunded)
	}
	if len(funded) != 1 {
		t.Fatalf("got funded=%+v, want one candidate", funded)
	}

	// units cap = capture rate * ADV * horizon = 0.20 * 100 * 3 = 60
	wantUnits := int64(60)
	// committed capital/unit = B*(1+broker) + S*broker = 1000*1.018 + 1100*0.018 = 1037.8
	wantCapital := 1037.8 * 60
	wantDaysToClear := 3.0 // units / (captureRate * ADV) = 60 / 20

	got := funded[0]
	if got.Units != wantUnits {
		t.Errorf("got Units=%d, want %d", got.Units, wantUnits)
	}
	if math.Abs(got.CommittedCapital-wantCapital) > 1e-6 {
		t.Errorf("got CommittedCapital=%v, want %v", got.CommittedCapital, wantCapital)
	}
	if math.Abs(got.DaysToClear-wantDaysToClear) > 1e-9 {
		t.Errorf("got DaysToClear=%v, want %v", got.DaysToClear, wantDaysToClear)
	}
}

func TestAllocatePartiallyFillsACandidateWhenBudgetFallsShortOfItsUnitsCap(t *testing.T) {
	ranked := []engine.Recommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100},
	}
	params := engine.AllocationParams{
		// capital/unit = 1037.8; units cap = 60 -> full fill needs 62,268.
		// Budget only covers 20,000, less than that.
		Budget:      20_000,
		OrderLimit:  21,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, unfunded := engine.Allocate(ranked, params)

	if len(unfunded) != 0 {
		t.Fatalf("got unfunded=%+v, want none (it should be a partial fill, not a skip)", unfunded)
	}
	if len(funded) != 1 {
		t.Fatalf("got funded=%+v, want one partially filled candidate", funded)
	}

	wantUnitsFloat := 20_000.0 / 1037.8 // 19.28...
	wantUnits := int64(wantUnitsFloat)  // floor -> 19
	wantCapital := 1037.8 * float64(wantUnits)

	got := funded[0]
	if got.Units != wantUnits {
		t.Errorf("got Units=%d, want %d", got.Units, wantUnits)
	}
	if math.Abs(got.CommittedCapital-wantCapital) > 1e-6 {
		t.Errorf("got CommittedCapital=%v, want %v", got.CommittedCapital, wantCapital)
	}
	if got.CommittedCapital >= 20_000 {
		t.Errorf("got CommittedCapital=%v, want it to stay within the %v budget", got.CommittedCapital, params.Budget)
	}
}

func TestAllocateLeavesAPartialFillBelowTheMinimumOrderIdleAndContinuesToTheNextCandidate(t *testing.T) {
	// Zero broker fee and CaptureRate/HorizonDays of 1 keep the arithmetic
	// simple: capital/unit == BuyPrice, units cap == AverageDailyVolume.
	// Density (spec \u00a710 step 1) is ExpectedDailyProfit/capital-per-unit,
	// so A (0.5) sorts ahead of B (0.1), ahead of C (0.04) -- shuffled here
	// to prove Allocate does its own sort rather than trusting input order.
	candidateA := engine.Recommendation{TypeID: 1, BuyPrice: 100, SellPrice: 110, AverageDailyVolume: 10, ExpectedDailyProfit: 50}
	candidateB := engine.Recommendation{TypeID: 2, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 60, ExpectedDailyProfit: 100}
	candidateC := engine.Recommendation{TypeID: 3, BuyPrice: 50, SellPrice: 55, AverageDailyVolume: 5, ExpectedDailyProfit: 2}
	ranked := []engine.Recommendation{candidateC, candidateB, candidateA}

	params := engine.AllocationParams{
		// A's full fill (10 units * 100) costs 1,000, leaving 500. B's full
		// fill would cost 60,000, so it gets whatever floor(500/1000)
		// buys -- zero units, below the 200 ISK minimum order: left idle,
		// not skipped, so the 500 remains available and reaches C, whose
		// full fill (5 units * 50) costs exactly 250 of it.
		Budget:      1_500,
		OrderLimit:  21,
		MinOrder:    200,
		CaptureRate: 1,
		HorizonDays: 1,
		Broker:      0,
	}

	funded, unfunded := engine.Allocate(ranked, params)

	if len(unfunded) != 1 || unfunded[0].TypeID != 2 || unfunded[0].Units != 0 {
		t.Fatalf("got unfunded=%+v, want only candidate B (2) left idle with zero units", unfunded)
	}
	if len(funded) != 2 {
		t.Fatalf("got funded=%+v, want candidates A and C funded", funded)
	}
	fundedByType := map[int32]engine.Recommendation{funded[0].TypeID: funded[0], funded[1].TypeID: funded[1]}
	if got := fundedByType[1]; got.Units != 10 || got.CommittedCapital != 1000 {
		t.Errorf("got candidate A funded=%+v, want Units=10, CommittedCapital=1000", got)
	}
	if got := fundedByType[3]; got.Units != 5 || got.CommittedCapital != 250 {
		t.Errorf("got candidate C funded=%+v, want Units=5, CommittedCapital=250 (reached using the budget candidate B left idle)", got)
	}
}

// TestAllocatePartialFillingCommitsMoreBudgetAndProfitThanWholeOrderSkipping
// checks the *shape* of the improvement the spec claims for partial filling
// (spec \u00a710: "the fractional-knapsack optimum for the budget constraint"),
// not specific reference numbers: a budget that lands mid-order for most
// candidates should let Allocate's partial fills commit materially more of
// the budget, and capture materially more expected daily profit, than a
// whole-order-skipping allocator that buys full units caps or nothing.
func TestAllocatePartialFillingCommitsMoreBudgetAndProfitThanWholeOrderSkipping(t *testing.T) {
	const budget = 150_000_000

	// C1's full order (96 units) costs ~99.7M, comfortably funded by
	// either strategy and leaving ~50.3M of the 150M budget. C2's full
	// order (240 units) costs ~498.6M -- far more than that remainder, so
	// a whole-order-skipping allocator strands the whole remainder,
	// while partial filling spends nearly all of it on a partial C2.
	ranked := []engine.Recommendation{
		{TypeID: 1, BuyPrice: 1_000_000, SellPrice: 1_150_000, AverageDailyVolume: 160, ExpectedDailyProfit: 10_387_000},
		{TypeID: 2, BuyPrice: 2_000_000, SellPrice: 2_300_000, AverageDailyVolume: 400, ExpectedDailyProfit: 2_077_400},
	}

	params := engine.AllocationParams{
		Budget:      budget,
		OrderLimit:  21,
		MinOrder:    1_000_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, _ := engine.Allocate(ranked, params)

	var partialCommitted, partialEDP float64
	for _, rec := range funded {
		partialCommitted += rec.CommittedCapital
		partialEDP += rec.ExpectedDailyProfit * params.CaptureRate
	}

	// A whole-order-skipping allocator: same density order, but a
	// candidate that doesn't fit its full units cap in the remaining
	// budget is skipped entirely rather than partially filled.
	var skipCommitted, skipEDP float64
	remaining := float64(budget)
	for _, rec := range ranked {
		unitsCap := int64(params.CaptureRate * rec.AverageDailyVolume * float64(params.HorizonDays))
		capitalPerUnit := rec.BuyPrice*(1+params.Broker) + rec.SellPrice*params.Broker
		fullCost := capitalPerUnit * float64(unitsCap)
		if fullCost <= remaining {
			remaining -= fullCost
			skipCommitted += fullCost
			skipEDP += rec.ExpectedDailyProfit * params.CaptureRate
		}
	}

	if partialCommitted <= skipCommitted {
		t.Errorf("got partial-fill committed=%v, skip committed=%v; want partial filling to commit materially more budget", partialCommitted, skipCommitted)
	}
	if partialEDP <= skipEDP {
		t.Errorf("got partial-fill EDP=%v, skip EDP=%v; want partial filling to capture materially more expected daily profit", partialEDP, skipEDP)
	}

	const materially = 1.05 // at least 5% better, not just noise
	if partialCommitted < skipCommitted*materially {
		t.Errorf("got partial-fill committed=%v, want at least %vx skip's %v", partialCommitted, materially, skipCommitted)
	}
	if partialEDP < skipEDP*materially {
		t.Errorf("got partial-fill EDP=%v, want at least %vx skip's %v", partialEDP, materially, skipEDP)
	}
}

func TestAllocateLeavesLaterCandidatesUnfundedOnceTheOrderLimitIsReached(t *testing.T) {
	// Each candidate costs two order slots (buy + sell); an order limit of
	// 2 admits exactly one candidate.
	ranked := []engine.Recommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100},
		{TypeID: 2, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100},
	}
	params := engine.AllocationParams{
		Budget:      150_000,
		OrderLimit:  2,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, unfunded := engine.Allocate(ranked, params)

	if len(funded) != 1 || funded[0].TypeID != 1 {
		t.Fatalf("got funded=%+v, want only type 1 funded", funded)
	}
	if len(unfunded) != 1 || unfunded[0].TypeID != 2 || unfunded[0].Units != 0 {
		t.Fatalf("got unfunded=%+v, want type 2 unfunded with zero units", unfunded)
	}
}
