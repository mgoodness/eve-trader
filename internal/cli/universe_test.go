package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// universeAndRouteFixtureServer serves the region-orders feed (one page)
// plus GET /route/{origin}/{destination}/: a reachable near system 3 jumps
// from Rens, and an unreachable one that 500s, so callers can exercise
// numeric jump-range coverage end to end (ticket #18).
func universeAndRouteFixtureServer(t *testing.T, orders []map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/markets/"):
			w.Header().Set("X-Pages", "1")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(orders)
		case strings.HasPrefix(r.URL.Path, "/route/30002187/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]int32{30002510, 30002200, 30002199, 30002187}) // 3 jumps
		case strings.HasPrefix(r.URL.Path, "/route/30099999/"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestTwoSidedUniverseCoversANumericRangeBuyOrderByExactJumpDistance(t *testing.T) {
	orders := []map[string]any{
		// type 11399 (Morphite): a station sell, and a buy order 3 jumps out
		// with a range of 5 \u2014 covers.
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004595, "system_id": 30002187, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "5"},
		// type 34 (Tritanium): a station sell, and a buy order 3 jumps out
		// with a range of 2 \u2014 doesn't cover.
		{"order_id": 3, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 5.0, "is_buy_order": false, "range": "region"},
		{"order_id": 4, "type_id": 34, "location_id": 60004595, "system_id": 30002187, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 3.0, "is_buy_order": true, "range": "2"},
	}
	server := universeAndRouteFixtureServer(t, orders)
	cfg := testConfig(t, server.URL)

	twoSided, warnings, err := cli.TwoSidedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("TwoSidedUniverse: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none (every route lookup succeeds)", warnings)
	}

	if len(twoSided) != 1 || twoSided[0].TypeID != 11399 {
		t.Fatalf("got two-sided types %+v, want only 11399 (Morphite: its buy order's 3-jump distance is within its range 5; Tritanium's is outside its range 2)", twoSided)
	}
}

func TestTwoSidedUniverseWarnsAndExcludesOnAFailedRouteLookup(t *testing.T) {
	orders := []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004596, "system_id": 30099999, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "40"},
	}
	server := universeAndRouteFixtureServer(t, orders)
	cfg := testConfig(t, server.URL)

	twoSided, warnings, err := cli.TwoSidedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("TwoSidedUniverse: %v", err)
	}

	if len(twoSided) != 0 {
		t.Fatalf("got two-sided types %+v, want none (the only buy order's route lookup failed, so it doesn't cover)", twoSided)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1 recording the failed route lookup: %v", len(warnings), warnings)
	}
}

func TestTwoSidedUniverseFetchesTheWholeFeedAndReportsTwoSidedTypesOnly(t *testing.T) {
	server := universeFixtureServer(t)
	cfg := testConfig(t, server.URL)

	twoSided, warnings, err := cli.TwoSidedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("TwoSidedUniverse: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none (no numeric-range orders in this fixture)", warnings)
	}

	if len(twoSided) != 1 {
		t.Fatalf("got %d two-sided types, want 1 (only Morphite: Tritanium is sell-only, Pyerite's only buy is at a structure): %+v", len(twoSided), twoSided)
	}
	if twoSided[0].TypeID != 11399 {
		t.Errorf("got type_id %d, want 11399 (Morphite)", twoSided[0].TypeID)
	}
}
