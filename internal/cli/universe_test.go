package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
)

func universeFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	const structureID int64 = 1031084757448 // 13-digit Upwell structure: excluded
	orders := []map[string]any{
		// type 11399 (Morphite): two-sided — a station sell, a covering region buy.
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
		// type 34 (Tritanium): sell-only, no covering buy.
		{"order_id": 3, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 5.0, "is_buy_order": false, "range": "region"},
		// type 35 (Pyerite): only buy order is at a structure, so it's excluded — not two-sided.
		{"order_id": 4, "type_id": 35, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 2.0, "is_buy_order": false, "range": "region"},
		{"order_id": 5, "type_id": 35, "location_id": structureID, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 1.0, "is_buy_order": true, "range": "region"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Pages", "1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestTwoSidedUniverseFetchesTheWholeFeedAndReportsTwoSidedTypesOnly(t *testing.T) {
	server := universeFixtureServer(t)
	cfg := testConfig(t, server.URL)

	twoSided, err := cli.TwoSidedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("TwoSidedUniverse: %v", err)
	}

	if len(twoSided) != 1 {
		t.Fatalf("got %d two-sided types, want 1 (only Morphite: Tritanium is sell-only, Pyerite's only buy is at a structure): %+v", len(twoSided), twoSided)
	}
	if twoSided[0].TypeID != 11399 {
		t.Errorf("got type_id %d, want 11399 (Morphite)", twoSided[0].TypeID)
	}
}
