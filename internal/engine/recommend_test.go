package engine_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

var testParams = engine.Params{
	RegionID:       10000030,
	TradeStationID: tradeStationID,
	TradeSystemID:  tradeSystemID,
	Delta:          100,
	Fees:           engine.Fees{Broker: 0.018, SalesTax: 0.05025},
}

func TestRecommendProducesOneRecommendationForATwoSidedCoveredBook(t *testing.T) {
	orders := []engine.Order{
		{OrderID: 1, TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		{OrderID: 2, TypeID: 11399, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 18220, Range: "station"},
	}
	generatedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	result := engine.Recommend(orders, 11399, "Morphite", testParams, nil, generatedAt)

	if len(result.Recommendations) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(result.Recommendations), result.Recommendations)
	}
	rec := result.Recommendations[0]
	if rec.BuyPrice != 18320 || rec.SellPrice != 23980 {
		t.Errorf("got buy_price=%v sell_price=%v, want 18320/23980", rec.BuyPrice, rec.SellPrice)
	}
	if len(result.Unfunded) != 0 || len(result.Excluded) != 0 {
		t.Errorf("got unfunded=%+v excluded=%+v, want both empty", result.Unfunded, result.Excluded)
	}
	if result.Summary.Recommendations != 1 || result.Summary.Excluded != 0 || result.Summary.Unfunded != 0 {
		t.Errorf("got summary %+v, want {Recommendations:1 Excluded:0 Unfunded:0}", result.Summary)
	}
	if result.Meta.RegionID != 10000030 || result.Meta.TradeStation != tradeStationID {
		t.Errorf("got meta %+v, want region_id=10000030 trade_station=%d", result.Meta, tradeStationID)
	}
	if result.Meta.Fees.Broker != 0.018 || result.Meta.Fees.SalesTax != 0.05025 {
		t.Errorf("got fees %+v, want {Broker:0.018 SalesTax:0.05025}", result.Meta.Fees)
	}
	if !result.Meta.GeneratedAt.Equal(generatedAt) {
		t.Errorf("got generated_at %v, want %v", result.Meta.GeneratedAt, generatedAt)
	}
}

func TestRecommendExcludesATypeWithNoTwoSidedBook(t *testing.T) {
	orders := []engine.Order{
		{OrderID: 1, TypeID: 11399, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 24080, Range: "region"},
		// no covering buy order
	}

	result := engine.Recommend(orders, 11399, "Morphite", testParams, nil, time.Now())

	if len(result.Recommendations) != 0 {
		t.Fatalf("got %d recommendations, want 0: %+v", len(result.Recommendations), result.Recommendations)
	}
	if len(result.Excluded) != 1 {
		t.Fatalf("got %d excluded, want 1: %+v", len(result.Excluded), result.Excluded)
	}
	if result.Excluded[0].TypeID != 11399 || result.Excluded[0].Reason == "" {
		t.Errorf("got excluded entry %+v, want type_id=11399 and a non-empty reason", result.Excluded[0])
	}
	if result.Summary.Excluded != 1 {
		t.Errorf("got summary.excluded=%d, want 1", result.Summary.Excluded)
	}
}

func TestRecommendExcludesACrossedBook(t *testing.T) {
	orders := []engine.Order{
		{OrderID: 1, TypeID: 34, IsBuyOrder: false, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 5, Range: "region"},
		{OrderID: 2, TypeID: 34, IsBuyOrder: true, LocationID: tradeStationID, SystemID: tradeSystemID, Price: 3, Range: "station"},
	}

	result := engine.Recommend(orders, 34, "Tritanium", testParams, nil, time.Now())

	if len(result.Recommendations) != 0 {
		t.Fatalf("got %d recommendations, want 0: %+v", len(result.Recommendations), result.Recommendations)
	}
	if len(result.Excluded) != 1 || result.Excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry with a non-empty reason", result.Excluded)
	}
}
