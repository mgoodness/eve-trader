package engine_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestNewResultSummarisesCommittedCapitalBudgetAndExpectedDailyProfitFromFunded(t *testing.T) {
	generatedAt := time.Date(2026, 9, 30, 11, 53, 0, 0, time.UTC)
	meta := engine.Meta{
		GeneratedAt:  generatedAt,
		RegionID:     10000030,
		TradeStation: 60004588,
		Params:       engine.RunParams{Budget: 150_000_000},
	}
	funded := []engine.BuyRecommendation{
		{TypeID: 1, CommittedCapital: 30_623_290, ExpectedDailyProfit: 10_039_779.96},
		{TypeID: 2, CommittedCapital: 79_707_210, ExpectedDailyProfit: 6_280_853.33},
	}

	result := engine.NewResult(funded, nil, nil, nil, nil, meta, engine.AllocationParams{Budget: 150_000_000, OrderLimit: 21})

	if result.Meta.GeneratedAt != generatedAt {
		t.Errorf("got GeneratedAt=%v, want %v", result.Meta.GeneratedAt, generatedAt)
	}
	if result.Summary.Recommendations != 2 {
		t.Errorf("got Recommendations=%d, want 2", result.Summary.Recommendations)
	}
	if result.Summary.CommittedCapital != 110_330_500 {
		t.Errorf("got CommittedCapital=%v, want 110330500", result.Summary.CommittedCapital)
	}
	if result.Summary.Budget != 150_000_000 {
		t.Errorf("got Budget=%v, want 150000000", result.Summary.Budget)
	}
	wantBudgetUsed := 110_330_500.0 / 150_000_000.0
	if result.Summary.BudgetUsed != wantBudgetUsed {
		t.Errorf("got BudgetUsed=%v, want %v", result.Summary.BudgetUsed, wantBudgetUsed)
	}
	if result.Summary.OrdersUsed != 2 {
		t.Errorf("got OrdersUsed=%d, want 2 (2 candidates \u00d7 1 order each)", result.Summary.OrdersUsed)
	}
	if result.Summary.OrderLimit != 21 {
		t.Errorf("got OrderLimit=%d, want 21", result.Summary.OrderLimit)
	}
	wantEDP := 10_039_779.96 + 6_280_853.33
	if math.Abs(result.Summary.ExpectedDailyProfit-wantEDP) > 1e-6 {
		t.Errorf("got ExpectedDailyProfit=%v, want %v", result.Summary.ExpectedDailyProfit, wantEDP)
	}
	if len(result.BuyRecommendations) != 2 {
		t.Errorf("got %d recommendations, want 2", len(result.BuyRecommendations))
	}
}

func TestNewResultSortsFundedAndUnfundedByExpectedDailyProfitDescending(t *testing.T) {
	funded := []engine.BuyRecommendation{
		{TypeID: 1, ExpectedDailyProfit: 100},
		{TypeID: 2, ExpectedDailyProfit: 500},
	}
	unfunded := []engine.BuyRecommendation{
		{TypeID: 3, ExpectedDailyProfit: 50},
		{TypeID: 4, ExpectedDailyProfit: 900},
	}

	result := engine.NewResult(funded, unfunded, nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, engine.AllocationParams{Budget: 1, OrderLimit: 21})

	if result.BuyRecommendations[0].TypeID != 2 || result.BuyRecommendations[1].TypeID != 1 {
		t.Fatalf("got funded order %+v, want type 2 (EDP 500) before type 1 (EDP 100)", result.BuyRecommendations)
	}
	if result.Unfunded[0].TypeID != 4 || result.Unfunded[1].TypeID != 3 {
		t.Fatalf("got unfunded order %+v, want type 4 (EDP 900) before type 3 (EDP 50)", result.Unfunded)
	}
}

func TestNewResultAccountsForUnfundedAndExcludedCounts(t *testing.T) {
	unfunded := []engine.BuyRecommendation{{TypeID: 9}, {TypeID: 10}}
	excluded := []engine.Excluded{{TypeID: 34, Name: "Tritanium", Reason: "thin book"}}

	result := engine.NewResult(nil, unfunded, excluded, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, engine.AllocationParams{Budget: 1, OrderLimit: 21})

	if result.Summary.Unfunded != 2 {
		t.Errorf("got Unfunded=%d, want 2", result.Summary.Unfunded)
	}
	if result.Summary.Excluded != 1 {
		t.Errorf("got Excluded=%d, want 1", result.Summary.Excluded)
	}
	if len(result.Unfunded) != 2 {
		t.Errorf("got %d unfunded, want 2", len(result.Unfunded))
	}
	if len(result.Excluded) != 1 || result.Excluded[0].Name != "Tritanium" {
		t.Errorf("got excluded=%+v, want Tritanium", result.Excluded)
	}
}

func TestNewResultRendersFlagsAsEmptyArraysNotNull(t *testing.T) {
	result := engine.NewResult(
		[]engine.BuyRecommendation{{TypeID: 1}},
		[]engine.BuyRecommendation{{TypeID: 2}},
		nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, engine.AllocationParams{Budget: 1, OrderLimit: 21},
	)

	if result.BuyRecommendations[0].Flags == nil {
		t.Errorf("got nil funded Flags, want a non-nil slice so JSON emits [] not null")
	}
	if result.Unfunded[0].Flags == nil {
		t.Errorf("got nil unfunded Flags, want a non-nil slice so JSON emits [] not null")
	}

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), `"flags":null`) {
		t.Errorf("got JSON containing \"flags\":null, want []:\n%s", body)
	}
	if !strings.Contains(string(body), `"flags":[]`) {
		t.Errorf("got JSON without \"flags\":[], want []:\n%s", body)
	}
}

func TestNewResultRendersEmptyResultCleanlyWithZeroBudgetUsed(t *testing.T) {
	result := engine.NewResult(nil, nil, nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 0}}, engine.AllocationParams{Budget: 0, OrderLimit: 21})

	if result.Summary.BudgetUsed != 0 {
		t.Errorf("got BudgetUsed=%v, want 0 (no division by zero with a zero budget)", result.Summary.BudgetUsed)
	}
	if result.BuyRecommendations == nil || result.Unfunded == nil || result.Excluded == nil || result.SellRecommendations == nil || result.Pending == nil {
		t.Errorf("got nil slice(s) in %+v, want empty slices so JSON emits [] not null", result)
	}
}

func TestNewResultCarriesSellRecommendationsAndPending(t *testing.T) {
	sells := []engine.SellRecommendation{{TypeID: 34, Quantity: 100}}
	pending := []engine.Pending{{TypeID: 35, Reason: engine.PendingAwaitingBuyFill, Quantity: 5}}

	result := engine.NewResult(nil, nil, nil, sells, pending, engine.Meta{Params: engine.RunParams{Budget: 1}}, engine.AllocationParams{Budget: 1, OrderLimit: 21})

	if len(result.SellRecommendations) != 1 || result.SellRecommendations[0].TypeID != 34 {
		t.Errorf("got SellRecommendations=%+v, want the supplied sell recommendation", result.SellRecommendations)
	}
	if len(result.Pending) != 1 || result.Pending[0].TypeID != 35 {
		t.Errorf("got Pending=%+v, want the supplied pending entry", result.Pending)
	}

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"sell_recommendations"`) || !strings.Contains(string(body), `"buy_recommendations"`) {
		t.Errorf("got JSON without the buy_recommendations/sell_recommendations keys:\n%s", body)
	}
}

func TestNewResultSplitsBudgetAndOrderLimitIntoReservedAndAvailable(t *testing.T) {
	// The summary reports the run's resource split (spec §13, §14): what
	// pre-existing open orders already reserved, what was therefore
	// available to allocate, and what this run actually used. Every
	// recommendation — buy or sell — costs one order slot (decision 4).
	funded := []engine.BuyRecommendation{{TypeID: 1, CommittedCapital: 30_000_000, ExpectedDailyProfit: 1}}
	sells := []engine.SellRecommendation{{TypeID: 34}}
	params := engine.AllocationParams{
		Budget:         150_000_000,
		OrderLimit:     21,
		ReservedSlots:  3,
		ReservedBudget: 40_000_000,
	}

	result := engine.NewResult(funded, nil, nil, sells, nil, engine.Meta{Params: engine.RunParams{Budget: 150_000_000}}, params)

	if result.Summary.BudgetReservedByExisting != 40_000_000 {
		t.Errorf("got BudgetReservedByExisting=%v, want 40000000", result.Summary.BudgetReservedByExisting)
	}
	if result.Summary.BudgetAvailable != 110_000_000 {
		t.Errorf("got BudgetAvailable=%v, want 110000000", result.Summary.BudgetAvailable)
	}
	if result.Summary.OrdersReservedByExisting != 3 {
		t.Errorf("got OrdersReservedByExisting=%d, want 3", result.Summary.OrdersReservedByExisting)
	}
	if result.Summary.OrdersAvailable != 18 {
		t.Errorf("got OrdersAvailable=%d, want 18", result.Summary.OrdersAvailable)
	}
	if result.Summary.OrderLimit != 21 {
		t.Errorf("got OrderLimit=%d, want 21", result.Summary.OrderLimit)
	}
	if result.Summary.OrdersUsed != 2 {
		t.Errorf("got OrdersUsed=%d, want 2 (one funded buy + one funded sell)", result.Summary.OrdersUsed)
	}
}

func TestNewResultNeverReportsNegativeAvailableResources(t *testing.T) {
	// A pilot whose stated budget is smaller than the escrow already tied
	// up in open buy orders has nothing left to allocate, not a negative
	// amount (spec §13, §14).
	params := engine.AllocationParams{
		Budget:         1_000_000,
		OrderLimit:     2,
		ReservedSlots:  5,
		ReservedBudget: 3_000_000,
	}

	result := engine.NewResult(nil, nil, nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1_000_000}}, params)

	if result.Summary.BudgetAvailable != 0 {
		t.Errorf("got BudgetAvailable=%v, want 0", result.Summary.BudgetAvailable)
	}
	if result.Summary.OrdersAvailable != 0 {
		t.Errorf("got OrdersAvailable=%d, want 0", result.Summary.OrdersAvailable)
	}
}

// TestNewResultJSONContractEmitsEmptyArraysNotNull confirms the stable
// machine contract (spec §14): every list is present and marshals as []
// rather than null on a zero-recommendation run, so an adapter can index it
// without a nil guard.
func TestNewResultJSONContractEmitsEmptyArraysNotNull(t *testing.T) {
	result := engine.NewResult(nil, nil, nil, nil, nil,
		engine.Meta{Params: engine.RunParams{Budget: 1}},
		engine.AllocationParams{Budget: 1, OrderLimit: 21})

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, key := range []string{
		`"buy_recommendations":[]`,
		`"unfunded":[]`,
		`"excluded":[]`,
		`"sell_recommendations":[]`,
		`"pending":[]`,
	} {
		if !strings.Contains(string(body), key) {
			t.Errorf("got JSON without %s:\n%s", key, body)
		}
	}
	if strings.Contains(string(body), `:null`) {
		t.Errorf("got JSON containing a null list, want []:\n%s", body)
	}
}

// TestNewResultJSONContractKeepsRoiPerDayAndPerLotSellDetail confirms the
// two pieces of the contract the default table deliberately omits: the buy
// side keeps roi_per_day for adapters, and each sell recommendation carries
// its per-lot detail (lot id, quantity, acquisition price).
func TestNewResultJSONContractKeepsRoiPerDayAndPerLotSellDetail(t *testing.T) {
	result := engine.NewResult(
		[]engine.BuyRecommendation{{TypeID: 11399, Name: "Morphite", RoiPerDay: 0.2}},
		nil, nil,
		[]engine.SellRecommendation{{
			TypeID: 34, Name: "Tritanium", Quantity: 980, SellPrice: 812, NetMargin: 0.42,
			PricedQuantity: 630, UnpricedQuantity: 350, NetProceeds: 1,
			Lots: []engine.SellLot{{LotID: "lot-1", Quantity: 630, AcquisitionPrice: engine.Float64Ptr(660)}},
		}}, nil,
		engine.Meta{Params: engine.RunParams{Budget: 1}},
		engine.AllocationParams{Budget: 1, OrderLimit: 21})

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"roi_per_day":0.2`) {
		t.Errorf("got JSON without roi_per_day, want it kept for adapters:\n%s", body)
	}
	wantLots := `"lots":[{"lot_id":"lot-1","quantity":630,"acquisition_price":660}]`
	if !strings.Contains(string(body), wantLots) {
		t.Errorf("got JSON without per-lot sell detail %s:\n%s", wantLots, body)
	}
}

// TestNewResultRendersEmptySellLotsAsEmptyArraysNotNull pins the same []-not-
// null contract one level down: a sell recommendation with no lots still
// marshals its lots array as [], not null (spec §14).
func TestNewResultRendersEmptySellLotsAsEmptyArraysNotNull(t *testing.T) {
	result := engine.NewResult(nil, nil, nil,
		[]engine.SellRecommendation{{TypeID: 34, Name: "Tritanium"}}, nil,
		engine.Meta{Params: engine.RunParams{Budget: 1}},
		engine.AllocationParams{Budget: 1, OrderLimit: 21})

	if result.SellRecommendations[0].Lots == nil {
		t.Errorf("got nil Lots, want a non-nil slice so JSON emits [] not null")
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(body), `"lots":null`) {
		t.Errorf("got JSON containing \"lots\":null, want []:\n%s", body)
	}
}
