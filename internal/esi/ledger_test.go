package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

// ledgerServer serves the three character-scope ledger routes with
// per-page bodies and records how many times each route was hit.
func ledgerServer(t *testing.T, orders, history, assets [][]map[string]any) (*httptest.Server, func(string) int) {
	t.Helper()
	hits := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("got Authorization %q, want %q", auth, "Bearer test-token")
		}

		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		idx := 0
		if page == "2" {
			idx = 1
		}

		switch {
		case strings.HasSuffix(r.URL.Path, "/orders/history/"):
			hits["history"]++
			w.Header().Set("X-Pages", "2")
			json.NewEncoder(w).Encode(history[idx])
		case strings.HasSuffix(r.URL.Path, "/orders/"):
			hits["orders"]++
			json.NewEncoder(w).Encode(orders[0])
		case strings.HasSuffix(r.URL.Path, "/assets/"):
			hits["assets"]++
			w.Header().Set("X-Pages", "2")
			json.NewEncoder(w).Encode(assets[idx])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server, func(kind string) int { return hits[kind] }
}

func ledgerClient(serverURL string) *esi.Client {
	return esi.NewClient(esi.ClientOptions{
		BaseURL:    serverURL,
		UserAgent:  "eve-trader/0.1 (test; +https://github.com/mgoodness/eve-trader)",
		CompatDate: "2026-09-30",
	})
}

func TestCharacterOrdersDecodesEveryField(t *testing.T) {
	orders := [][]map[string]any{{
		{
			"order_id": 101, "type_id": 34, "region_id": 10000030, "location_id": 60004588,
			"range": "station", "is_buy_order": true, "is_corporation": false,
			"price": 12.5, "volume_total": 100, "volume_remain": 60, "min_volume": 1,
			"duration": 30, "issued": "2024-01-02T03:04:05Z", "escrow": 750.0,
		},
	}}
	server, _ := ledgerServer(t, orders, [][]map[string]any{{}}, [][]map[string]any{{}})

	got, err := ledgerClient(server.URL).CharacterOrders(t.Context(), 932683762, "test-token")
	if err != nil {
		t.Fatalf("CharacterOrders: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d orders, want 1", len(got))
	}
	o := got[0]
	if o.OrderID != 101 || o.TypeID != 34 || o.RegionID != 10000030 || o.LocationID != 60004588 {
		t.Errorf("got %+v, want the wire ids decoded", o)
	}
	if !o.IsBuyOrder || o.Price != 12.5 || o.VolumeTotal != 100 || o.VolumeRemain != 60 || o.MinVolume != 1 {
		t.Errorf("got %+v, want the wire numbers decoded", o)
	}
	if o.Range != "station" || o.Duration != 30 || o.Escrow != 750.0 {
		t.Errorf("got %+v, want range/duration/escrow decoded", o)
	}
	if o.Issued.IsZero() {
		t.Errorf("got zero Issued, want the wire timestamp")
	}
}

func TestCharacterOrderHistoryPaginatesAndCarriesState(t *testing.T) {
	history := [][]map[string]any{
		{{"order_id": 1, "type_id": 34, "state": "cancelled"}},
		{{"order_id": 2, "type_id": 34, "state": "expired"}},
	}
	server, hits := ledgerServer(t, [][]map[string]any{{}}, history, [][]map[string]any{{}})

	got, err := ledgerClient(server.URL).CharacterOrderHistory(t.Context(), 932683762, "test-token")
	if err != nil {
		t.Fatalf("CharacterOrderHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d history rows, want 2 (both pages)", len(got))
	}
	if got[0].State != "cancelled" || got[1].State != "expired" {
		t.Errorf("got states %q, %q, want cancelled, expired", got[0].State, got[1].State)
	}
	if hits("history") != 2 {
		t.Errorf("got %d history requests, want 2 (one per page)", hits("history"))
	}
}

func TestCharacterAssetsPaginatesAndDecodesLocationFields(t *testing.T) {
	assets := [][]map[string]any{
		{{"item_id": 1, "type_id": 34, "quantity": 10, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar", "is_singleton": false}},
		{{"item_id": 2, "type_id": 35, "quantity": 5, "location_id": 60004588, "location_type": "station", "location_flag": "Cargo", "is_singleton": true}},
	}
	server, hits := ledgerServer(t, [][]map[string]any{{}}, [][]map[string]any{{}}, assets)

	got, err := ledgerClient(server.URL).CharacterAssets(t.Context(), 932683762, "test-token")
	if err != nil {
		t.Fatalf("CharacterAssets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d assets, want 2 (both pages)", len(got))
	}
	if got[0].ItemID != 1 || got[0].TypeID != 34 || got[0].Quantity != 10 || got[0].LocationFlag != "Hangar" {
		t.Errorf("got %+v, want the wire asset decoded", got[0])
	}
	if !got[1].IsSingleton || got[1].LocationType != "station" {
		t.Errorf("got %+v, want is_singleton/location_type decoded", got[1])
	}
	if hits("assets") != 2 {
		t.Errorf("got %d asset requests, want 2 (one per page)", hits("assets"))
	}
}
