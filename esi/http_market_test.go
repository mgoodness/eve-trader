package esi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchRensOrdersPaginatesAndResolvesNamesOnce(t *testing.T) {
	var orderPages = map[string]any{
		"1": []map[string]any{{"order_id": 1, "type_id": 34, "location_id": 60004588, "issued": "2026-09-16T00:00:00Z"}},
		"2": []map[string]any{{"order_id": 2, "type_id": 34, "location_id": 60004588, "issued": "2026-09-16T00:00:00Z"}},
	}
	var typeRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest/markets/10000030/orders/":
			page := r.URL.Query().Get("page")
			w.Header().Set("X-Pages", "2")
			if err := json.NewEncoder(w).Encode(orderPages[page]); err != nil {
				t.Fatal(err)
			}
		case "/latest/universe/types/34/":
			typeRequests++
			json.NewEncoder(w).Encode(map[string]string{"name": "Tritanium"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchRensOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Tritanium" || got[1].Name != "Tritanium" {
		t.Fatalf("orders = %+v", got)
	}
	if typeRequests != 1 {
		t.Fatalf("type lookup requests = %d, want 1", typeRequests)
	}
}

func TestHTTPGatewayFetchHistoryRejectsMalformedDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"date":"not-a-date","volume":1,"order_count":1}]`)
	}))
	defer server.Close()

	if _, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchHistory(context.Background(), 34); err == nil {
		t.Fatal("FetchHistory() error = nil, want malformed-date error")
	}
}
