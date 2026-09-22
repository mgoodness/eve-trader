package esi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchCharacterOrdersPaginatesAndSendsAuth(t *testing.T) {
	pages := map[string][]map[string]any{
		"1": {
			{
				"order_id": 1, "type_id": 34, "location_id": 60004588, "is_buy_order": true,
				"price": 5.5, "volume_remain": 100, "volume_total": 200, "min_volume": 1,
				"issued": "2026-09-16T00:00:00Z", "duration": 90, "is_corporation": false,
				"region_id": 10000030, "range": "station", "escrow": 550.0,
			},
		},
		"2": {
			{
				"order_id": 2, "type_id": 35, "location_id": 60004588, "is_buy_order": false,
				"price": 6.0, "volume_remain": 5, "volume_total": 5,
				"issued": "2026-09-17T00:00:00Z", "duration": 90, "is_corporation": false,
				"region_id": 10000030, "range": "region",
			},
		},
	}
	var gotAuth, gotPage string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/orders/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotPage = r.URL.Query().Get("page")
		w.Header().Set("X-Pages", "2")
		if err := json.NewEncoder(w).Encode(pages[gotPage]); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchCharacterOrders(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer access" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if len(got) != 2 {
		t.Fatalf("orders = %+v, want 2 across both pages", got)
	}
	buy := got[0]
	if buy.OrderID != 1 || buy.TypeID != 34 || buy.LocationID != 60004588 || !buy.IsBuyOrder ||
		buy.Price != 5.5 || buy.VolumeRemain != 100 || buy.VolumeTotal != 200 || buy.MinVolume != 1 ||
		buy.Duration != 90 || buy.IsCorporation || buy.RegionID != 10000030 || buy.Range != "station" || buy.Escrow != 550.0 {
		t.Fatalf("open buy order = %+v", buy)
	}
	if buy.State != "" {
		t.Fatalf("open order state = %q, want empty (the open route carries no state)", buy.State)
	}
	sell := got[1]
	if sell.OrderID != 2 || sell.IsBuyOrder || sell.State != "" {
		t.Fatalf("open sell order = %+v", sell)
	}
}

// TestHTTPGatewayFetchCharacterOrderHistoryMissingIsBuyOrderIsSell covers
// the load-bearing order-history quirk (docs/spec/v2.md §3): ESI omits
// is_buy_order for sell orders, so a history row with no is_buy_order must
// decode as a sell (false), while an explicit true stays a buy.
func TestHTTPGatewayFetchCharacterOrderHistoryMissingIsBuyOrderIsSell(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/orders/history/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("X-Pages", "1")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"order_id": 10, "type_id": 626, "location_id": 60004588, "is_buy_order": true,
				"price": 12790000, "volume_remain": 1, "volume_total": 1,
				"issued": "2026-07-24T02:13:39Z", "duration": 0, "state": "cancelled",
				"is_corporation": false, "region_id": 10000030, "range": "station",
			},
			{
				"order_id": 11, "type_id": 626, "location_id": 60004588,
				"price": 12790000, "volume_remain": 1, "volume_total": 1,
				"issued": "2026-07-24T02:13:39Z", "duration": 0, "state": "cancelled",
				"is_corporation": false, "region_id": 10000030, "range": "station",
			},
		})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchCharacterOrderHistory(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history = %+v, want 2", got)
	}
	if !got[0].IsBuyOrder {
		t.Fatalf("explicit is_buy_order true decoded as %v, want true", got[0].IsBuyOrder)
	}
	if got[1].IsBuyOrder {
		t.Fatalf("missing is_buy_order decoded as buy, want sell (false)")
	}
	if got[1].State != "cancelled" {
		t.Fatalf("state = %q, want cancelled", got[1].State)
	}
}
