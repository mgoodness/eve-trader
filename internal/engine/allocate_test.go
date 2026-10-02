package engine_test

import (
	"math"
	"slices"
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestAllocateFlagsAPartialFill(t *testing.T) {
	// units cap = 0.20*100*3 = 60; a 20,000 budget covers only ~19 units, so
	// the allocation is a partial fill and must say so (spec §10 step 4, §11
	// flags).
	rec := engine.BuyRecommendation{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100, ExpectedDailyProfit: 10}
	params := engine.AllocationParams{
		Budget:      20_000,
		OrderLimit:  21,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, _ := engine.Allocate([]engine.BuyRecommendation{rec}, params)

	if len(funded) != 1 {
		t.Fatalf("got funded=%+v, want one partially filled candidate", funded)
	}
	if funded[0].Units >= 60 {
		t.Fatalf("got Units=%d, want fewer than the 60-unit cap for this to be a partial fill", funded[0].Units)
	}
	if !slices.Contains(funded[0].Flags, engine.FlagPartialFill) {
		t.Errorf("got Flags=%v, want it to contain %q", funded[0].Flags, engine.FlagPartialFill)
	}
}

func TestAllocateDoesNotFlagAFullFill(t *testing.T) {
	rec := engine.BuyRecommendation{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100, ExpectedDailyProfit: 10}
	params := engine.AllocationParams{
		Budget:      150_000,
		OrderLimit:  21,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	funded, _ := engine.Allocate([]engine.BuyRecommendation{rec}, params)

	if len(funded) != 1 {
		t.Fatalf("got funded=%+v, want one candidate", funded)
	}
	if funded[0].Units != 60 {
		t.Fatalf("got Units=%d, want the full 60-unit cap", funded[0].Units)
	}
	if slices.Contains(funded[0].Flags, engine.FlagPartialFill) {
		t.Errorf("got Flags=%v, want no partial-fill flag on a full fill", funded[0].Flags)
	}
	if funded[0].Flags == nil {
		t.Errorf("got nil Flags, want a non-nil slice so JSON emits [] not null")
	}
}

func TestCapitalNeededFloorsEachBrokerLegAtTheMinimumOrderFee(t *testing.T) {
	rec := engine.BuyRecommendation{BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 1}

	// units cap = 1 * 1 * 5 = 5, giving an order value of 5,000 ISK: each
	// leg's percentage broker fee (90 and 99 ISK) is below the 100 ISK
	// per-order minimum (spec §4, §8), so each leg is charged 100.
	// committed = 5*1000 + 100 + 100 = 5,200.
	got := engine.CapitalNeeded(rec, 0.018, 1, 5)
	if math.Abs(got-5200) > 1e-6 {
		t.Errorf("got CapitalNeeded=%v, want 5200 (both broker legs floored at 100 ISK)", got)
	}
}

func TestAllocateFloorsEachBrokerLegAtTheMinimumOrderFee(t *testing.T) {
	// cap = 1 * 1 * 5 = 5 units; the 3,500 budget affords only 3 units. At
	// that order value each leg's percentage fee (54, 59.4 ISK) is below the
	// 100 ISK floor, so committed capital is 3*1000 + 100 + 100 = 3,200.
	rec := engine.BuyRecommendation{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 1, ExpectedDailyProfit: 5}
	params := engine.AllocationParams{
		Budget:      3_500,
		OrderLimit:  21,
		MinOrder:    1,
		CaptureRate: 1,
		HorizonDays: 5,
		Broker:      0.018,
	}

	funded, unfunded := engine.Allocate([]engine.BuyRecommendation{rec}, params)

	if len(unfunded) != 0 {
		t.Fatalf("got unfunded=%+v, want none", unfunded)
	}
	if len(funded) != 1 {
		t.Fatalf("got funded=%+v, want one candidate", funded)
	}
	got := funded[0]
	if got.Units != 3 {
		t.Errorf("got Units=%d, want 3", got.Units)
	}
	if math.Abs(got.CommittedCapital-3200) > 1e-6 {
		t.Errorf("got CommittedCapital=%v, want 3200 (both broker legs floored at 100 ISK)", got.CommittedCapital)
	}
}

func TestCapitalNeededReportsWhatACandidateWouldNeedToBeFundedAtItsUnitsCap(t *testing.T) {
	rec := engine.BuyRecommendation{BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100}

	// units cap = 0.20 * 100 * 3 = 60; capital/unit = 1000*1.018 + 1100*0.018 = 1037.8
	want := 1037.8 * 60
	got := engine.CapitalNeeded(rec, 0.018, 0.20, 3)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("got CapitalNeeded=%v, want %v", got, want)
	}
}

func TestAllocateFundsACandidateAtItsUnitsCapWhenBudgetComfortablyCoversIt(t *testing.T) {
	ranked := []engine.BuyRecommendation{
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
	wantDaysOfSupply := 3.0 // units / (captureRate * ADV) = 60 / 20

	got := funded[0]
	if got.Units != wantUnits {
		t.Errorf("got Units=%d, want %d", got.Units, wantUnits)
	}
	if math.Abs(got.CommittedCapital-wantCapital) > 1e-6 {
		t.Errorf("got CommittedCapital=%v, want %v", got.CommittedCapital, wantCapital)
	}
	if math.Abs(got.DaysOfSupply-wantDaysOfSupply) > 1e-9 {
		t.Errorf("got DaysOfSupply=%v, want %v", got.DaysOfSupply, wantDaysOfSupply)
	}
}

func TestAllocatePartiallyFillsACandidateWhenBudgetFallsShortOfItsUnitsCap(t *testing.T) {
	ranked := []engine.BuyRecommendation{
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
	// CaptureRate/HorizonDays of 1 keep the units cap equal to
	// AverageDailyVolume, and a zero broker rate isolates the per-order
	// 100 ISK floor (spec §4, §8): every posted order's committed capital is
	// units×BuyPrice + 100 (buy leg) + 100 (sell leg). Density (spec §10 step
	// 1) is ExpectedDailyProfit/capital-per-unit, so A sorts ahead of B,
	// ahead of C -- shuffled here to prove Allocate does its own sort rather
	// than trusting input order.
	candidateA := engine.BuyRecommendation{TypeID: 1, BuyPrice: 100, SellPrice: 110, AverageDailyVolume: 10, ExpectedDailyProfit: 50}
	candidateB := engine.BuyRecommendation{TypeID: 2, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 60, ExpectedDailyProfit: 100}
	candidateC := engine.BuyRecommendation{TypeID: 3, BuyPrice: 50, SellPrice: 55, AverageDailyVolume: 5, ExpectedDailyProfit: 2}
	ranked := []engine.BuyRecommendation{candidateC, candidateB, candidateA}

	params := engine.AllocationParams{
		// A's full fill (10 units * 100, plus both 100 ISK broker floors)
		// costs 1,200, leaving 300. B's cheapest legal order (one unit:
		// 1,000 plus both floors) costs 1,200 > 300, so it gets zero units --
		// below the 200 ISK minimum order: left idle, not skipped, so the 300
		// remains available and reaches C, whose 2 units (2*50 plus both
		// floors) cost exactly 300 of it.
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
	fundedByType := map[int32]engine.BuyRecommendation{funded[0].TypeID: funded[0], funded[1].TypeID: funded[1]}
	if got := fundedByType[1]; got.Units != 10 || got.CommittedCapital != 1200 {
		t.Errorf("got candidate A funded=%+v, want Units=10, CommittedCapital=1200", got)
	}
	if got := fundedByType[3]; got.Units != 2 || got.CommittedCapital != 300 {
		t.Errorf("got candidate C funded=%+v, want Units=2, CommittedCapital=300 (reached using the budget candidate B left idle)", got)
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
	ranked := []engine.BuyRecommendation{
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
	// Each recommendation now costs one order slot (decision 3: a single
	// buy or sell order, spec §13), so an order limit of 1 admits exactly
	// one candidate.
	ranked := []engine.BuyRecommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100},
		{TypeID: 2, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100},
	}
	params := engine.AllocationParams{
		Budget:      150_000,
		OrderLimit:  1,
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

func TestAllocateWithSellsGivesSellRecommendationsSlotsBeforeNewBuys(t *testing.T) {
	// One sell recommendation and one buy candidate compete for a single
	// remaining order slot (spec §13 step 3; ADR 0007): the sell recovers
	// already-spent capital, so it takes the slot and the buy is left
	// unfunded even though the budget comfortably covers it.
	sells := []engine.SellRecommendation{{TypeID: 34, Name: "Tritanium", Quantity: 100}}
	buys := []engine.BuyRecommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100, ExpectedDailyProfit: 10},
	}
	params := engine.AllocationParams{
		Budget:      150_000,
		OrderLimit:  1,
		MinOrder:    1_000,
		CaptureRate: 0.20,
		HorizonDays: 3,
		Broker:      0.018,
	}

	fundedSells, pendingSells, fundedBuys, unfundedBuys := engine.AllocateWithSells(sells, buys, params)

	if len(fundedSells) != 1 || fundedSells[0].TypeID != 34 {
		t.Fatalf("got fundedSells=%+v, want the sell recommendation funded first", fundedSells)
	}
	if len(pendingSells) != 0 {
		t.Errorf("got pendingSells=%+v, want none", pendingSells)
	}
	if len(fundedBuys) != 0 {
		t.Errorf("got fundedBuys=%+v, want none once the sell took the only slot", fundedBuys)
	}
	if len(unfundedBuys) != 1 || unfundedBuys[0].TypeID != 1 || unfundedBuys[0].Units != 0 {
		t.Fatalf("got unfundedBuys=%+v, want the buy left unfunded with zero units", unfundedBuys)
	}
}

func TestAllocateWithSellsMarksExcessSellRecommendationsPendingWithTheOrderLimitReason(t *testing.T) {
	// Three sell recommendations, two order slots, no reserved resources:
	// the first two claim slots in their existing order and the third is
	// pending order-limit-exhausted rather than silently dropped (spec §11,
	// §13 step 3; ADR 0007).
	sells := []engine.SellRecommendation{
		{TypeID: 1, Name: "first", Quantity: 10},
		{TypeID: 2, Name: "second", Quantity: 20},
		{TypeID: 3, Name: "third", Quantity: 30},
	}
	params := engine.AllocationParams{OrderLimit: 2}

	fundedSells, pendingSells, _, _ := engine.AllocateWithSells(sells, nil, params)

	if len(fundedSells) != 2 || fundedSells[0].TypeID != 1 || fundedSells[1].TypeID != 2 {
		t.Fatalf("got fundedSells=%+v, want the first two in order", fundedSells)
	}
	if len(pendingSells) != 1 {
		t.Fatalf("got pendingSells=%+v, want exactly one pending entry", pendingSells)
	}
	got := pendingSells[0]
	if got.TypeID != 3 || got.Quantity != 30 {
		t.Errorf("got pending %+v, want type 3 with its 30 held units", got)
	}
	if got.Reason != engine.PendingOrderLimitExhausted {
		t.Errorf("got Reason=%q, want %q", got.Reason, engine.PendingOrderLimitExhausted)
	}
	if got.Detail == "" {
		t.Errorf("got empty Detail, want a human-readable reason the slot was withheld")
	}
}

func TestAllocateWithSellsShrinksHeadroomByReservedSlotsBeforeSellsAndBuys(t *testing.T) {
	// One pre-existing open order reserves a slot, leaving one of the two
	// for this run. The sell takes it; without the reservation the buy would
	// also have been funded.
	sells := []engine.SellRecommendation{{TypeID: 34, Name: "Tritanium", Quantity: 100}}
	buys := []engine.BuyRecommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100, ExpectedDailyProfit: 10},
	}
	params := engine.AllocationParams{
		Budget:        150_000,
		OrderLimit:    2,
		MinOrder:      1_000,
		CaptureRate:   0.20,
		HorizonDays:   3,
		Broker:        0.018,
		ReservedSlots: 1,
	}

	fundedSells, _, fundedBuys, unfundedBuys := engine.AllocateWithSells(sells, buys, params)

	if len(fundedSells) != 1 {
		t.Fatalf("got fundedSells=%+v, want the sell funded from the one free slot", fundedSells)
	}
	if len(fundedBuys) != 0 {
		t.Errorf("got fundedBuys=%+v, want none: the reserved slot plus the sell exhaust the limit", fundedBuys)
	}
	if len(unfundedBuys) != 1 {
		t.Fatalf("got unfundedBuys=%+v, want the buy unfunded", unfundedBuys)
	}
}

func TestAllocateWithSellsShrinksTheBuyBudgetByReservedBuyEscrow(t *testing.T) {
	// The whole stated budget is already escrowed against an open buy order
	// (spec §13 step 2), so no new buy gets committed capital even with
	// slots and a positive stated budget.
	buys := []engine.BuyRecommendation{
		{TypeID: 1, BuyPrice: 1000, SellPrice: 1100, AverageDailyVolume: 100, ExpectedDailyProfit: 10},
	}
	params := engine.AllocationParams{
		Budget:         100_000,
		OrderLimit:     5,
		MinOrder:       1_000,
		CaptureRate:    0.20,
		HorizonDays:    3,
		Broker:         0.018,
		ReservedBudget: 100_000,
	}

	fundedSells, pendingSells, fundedBuys, unfundedBuys := engine.AllocateWithSells(nil, buys, params)

	if len(fundedSells) != 0 || len(pendingSells) != 0 {
		t.Errorf("got fundedSells=%+v pendingSells=%+v, want none", fundedSells, pendingSells)
	}
	if len(fundedBuys) != 0 {
		t.Errorf("got fundedBuys=%+v, want none once the reserved escrow consumed the budget", fundedBuys)
	}
	if len(unfundedBuys) != 1 || unfundedBuys[0].Units != 0 {
		t.Fatalf("got unfundedBuys=%+v, want the buy unfunded with zero units", unfundedBuys)
	}
}

// TestReservedResourcesCountsEveryOpenOrderAndOnlyBuyEscrow pins spec §13
// steps 1–2: every open order reserves an order-limit slot, and only open buy
// orders reserve budget, at price × volume_remain.
func TestReservedResourcesCountsEveryOpenOrderAndOnlyBuyEscrow(t *testing.T) {
	orders := []engine.CharacterOrder{
		{OrderID: 1, IsBuyOrder: true, Price: 200, VolumeRemain: 50},
		{OrderID: 2, IsBuyOrder: false, Price: 300, VolumeRemain: 100},
		{OrderID: 3, IsBuyOrder: true, Price: 1000, VolumeRemain: 5},
	}

	slots, budget := engine.ReservedResources(orders)

	if slots != 3 {
		t.Errorf("got slots=%d, want 3 (every open order reserves one)", slots)
	}
	want := 200.0*50 + 1000.0*5
	if budget != want {
		t.Errorf("got budget=%v, want %v (only open buys escrow, open sells do not)", budget, want)
	}
}
