package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestRegionOrdersFetchesAllPagesAndFiltersByType(t *testing.T) {
	page1 := []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
	}
	page2 := []map[string]any{
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
	}

	var gotPaths []string
	var gotCompatDate, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.RequestURI())
		gotCompatDate = r.Header.Get("X-Compatibility-Date")
		gotUserAgent = r.Header.Get("User-Agent")

		w.Header().Set("X-Pages", "2")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "", "1":
			json.NewEncoder(w).Encode(page1)
		case "2":
			json.NewEncoder(w).Encode(page2)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    server.URL,
		UserAgent:  "eve-trader/0.1 (test; +https://github.com/mgoodness/eve-trader)",
		CompatDate: "2026-09-30",
	})

	orders, err := client.RegionOrders(t.Context(), 10000030, "all", 11399)
	if err != nil {
		t.Fatalf("RegionOrders: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("got %d orders, want 2: %+v", len(orders), orders)
	}
	if orders[0].OrderID != 1 || orders[1].OrderID != 2 {
		t.Errorf("got order IDs %d, %d, want 1, 2", orders[0].OrderID, orders[1].OrderID)
	}
	if orders[1].IsBuyOrder != true || orders[1].Price != 18220 {
		t.Errorf("got order[1]=%+v, want a buy order at price 18220", orders[1])
	}

	if len(gotPaths) != 2 {
		t.Fatalf("got %d requests, want 2 (one per page): %v", len(gotPaths), gotPaths)
	}
	for _, p := range gotPaths {
		if want := "type_id=11399"; !strings.Contains(p, want) {
			t.Errorf("request %q missing %q", p, want)
		}
		if want := "order_type=all"; !strings.Contains(p, want) {
			t.Errorf("request %q missing %q", p, want)
		}
	}
	if gotCompatDate != "2026-09-30" {
		t.Errorf("got X-Compatibility-Date %q, want %q", gotCompatDate, "2026-09-30")
	}
	if gotUserAgent == "" {
		t.Errorf("got empty User-Agent, want a set contact string")
	}
}
