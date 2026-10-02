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

	result := engine.NewResult(funded, nil, nil, nil, nil, meta, 21)

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
	if result.Summary.OrdersUsed != 4 {
		t.Errorf("got OrdersUsed=%d, want 4 (2 candidates \u00d7 2 orders each)", result.Summary.OrdersUsed)
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

	result := engine.NewResult(funded, unfunded, nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, 21)

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

	result := engine.NewResult(nil, unfunded, excluded, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, 21)

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
		nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 1}}, 21,
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
	result := engine.NewResult(nil, nil, nil, nil, nil, engine.Meta{Params: engine.RunParams{Budget: 0}}, 21)

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

	result := engine.NewResult(nil, nil, nil, sells, pending, engine.Meta{Params: engine.RunParams{Budget: 1}}, 21)

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
