package scanner

import (
	"context"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

func TestFetchHubPricesFindsBestBuyAndSell(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 5.60},
		{OrderID: 2, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 5.45}, // best sell (lowest)
		{OrderID: 3, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: true, Price: 5.00},
		{OrderID: 4, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: true, Price: 5.10}, // best buy (highest)
		{OrderID: 5, TypeID: 35, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 11.0},
		{OrderID: 6, TypeID: 34, LocationID: hub.Rens.StationID, IsBuyOrder: false, Price: 0.01},       // different hub, ignored
		{OrderID: 7, TypeID: 99999999, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 0.01}, // not in item universe, ignored
	}

	prices, err := FetchHubPrices(context.Background(), fake, hub.Jita, []int32{34, 35})
	if err != nil {
		t.Fatalf("FetchHubPrices: %v", err)
	}

	p34, ok := prices[34]
	if !ok {
		t.Fatal("prices[34] missing")
	}
	if !p34.HasBestSell || p34.BestSell != 5.45 {
		t.Errorf("prices[34].BestSell = %v (has=%v), want 5.45", p34.BestSell, p34.HasBestSell)
	}
	if !p34.HasBestBuy || p34.BestBuy != 5.10 {
		t.Errorf("prices[34].BestBuy = %v (has=%v), want 5.10", p34.BestBuy, p34.HasBestBuy)
	}

	p35, ok := prices[35]
	if !ok {
		t.Fatal("prices[35] missing")
	}
	if !p35.HasBestSell || p35.BestSell != 11.0 {
		t.Errorf("prices[35].BestSell = %v (has=%v), want 11.0", p35.BestSell, p35.HasBestSell)
	}
	if p35.HasBestBuy {
		t.Errorf("prices[35].HasBestBuy = true, want false (no buy orders for 35)")
	}

	if _, ok := prices[99999999]; ok {
		t.Error("prices contains an item outside the requested universe")
	}
}

// TestFetchHubPricesQueriesEachItemSeparately: FetchHubPrices must scope
// each request to one item type (ESI's type_id filter) rather than
// sweeping the whole region -- confirmed live, where an unfiltered sweep
// of Jita's order book ran 400+ pages and never completed within a
// dashboard request. MarketOrdersByType only returns a match when the
// requested typeID equals the map key, so this fails if FetchHubPrices
// stops passing typeID through.
func TestFetchHubPricesQueriesEachItemSeparately(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersByType = map[int32][]esi.MarketOrder{
		34: {{OrderID: 1, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 5.45}},
		35: {{OrderID: 2, TypeID: 35, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 11.0}},
	}

	prices, err := FetchHubPrices(context.Background(), fake, hub.Jita, []int32{34, 35})
	if err != nil {
		t.Fatalf("FetchHubPrices: %v", err)
	}

	if p, ok := prices[34]; !ok || !p.HasBestSell || p.BestSell != 5.45 {
		t.Errorf("prices[34] = %+v (ok=%v), want BestSell 5.45", p, ok)
	}
	if p, ok := prices[35]; !ok || !p.HasBestSell || p.BestSell != 11.0 {
		t.Errorf("prices[35] = %+v (ok=%v), want BestSell 11.0", p, ok)
	}
}

func TestFetchHubPricesNoOrders(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = nil

	prices, err := FetchHubPrices(context.Background(), fake, hub.Jita, []int32{34})
	if err != nil {
		t.Fatalf("FetchHubPrices: %v", err)
	}
	if len(prices) != 0 {
		t.Errorf("prices = %+v, want empty", prices)
	}
}

func TestFetchHubPricesPropagatesError(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersErr = context.DeadlineExceeded

	if _, err := FetchHubPrices(context.Background(), fake, hub.Jita, []int32{34}); err == nil {
		t.Fatal("FetchHubPrices: want error, got nil")
	}
}
