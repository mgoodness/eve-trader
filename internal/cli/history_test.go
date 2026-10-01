package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
)

// historyDays builds n consecutive daily history records starting
// 2024-01-01, each with the given volume, highest, and lowest.
func historyDays(n int, volume int, highest, lowest float64) []map[string]any {
	records := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, map[string]any{
			"date":        fmt.Sprintf("2024-01-%02d", i+1),
			"order_count": 1,
			"volume":      volume,
			"highest":     highest,
			"average":     (highest + lowest) / 2,
			"lowest":      lowest,
		})
	}
	return records
}

// fixtureTypeNames is the small type-id → name map the fake ESI servers
// resolve POST /universe/names/ against, so tests can assert the pipeline
// populates the output contract's `name` field (spec §11).
var fixtureTypeNames = map[int32]string{
	11399: "Morphite",
	34:    "Tritanium",
}

// historyFilteredFixtureServer extends bookFilteredFixtureServer with the
// region's market-history route, keyed by type_id, and records every
// type_id a history request was made for (regardless of whether it was
// served), so a test can prove history is fetched only for book-only
// survivors and at most once per call.
func historyFilteredFixtureServer(t *testing.T, orders []map[string]any, history map[int32][]map[string]any) (server *httptest.Server, historyRequests func() []int32) {
	t.Helper()
	var mu sync.Mutex
	var requested []int32

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/history"):
			typeID, _ := strconv.Atoi(r.URL.Query().Get("type_id"))
			mu.Lock()
			requested = append(requested, int32(typeID))
			mu.Unlock()
			json.NewEncoder(w).Encode(history[int32(typeID)])
		case r.URL.Path == "/universe/names/":
			var ids []int32
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Errorf("decoding /universe/names/ request: %v", err)
			}
			resolved := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				if name, ok := fixtureTypeNames[id]; ok {
					resolved = append(resolved, map[string]any{"id": id, "name": name, "category": "inventory_type"})
				}
			}
			json.NewEncoder(w).Encode(resolved)
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.Contains(r.URL.Path, "/orders"):
			w.Header().Set("X-Pages", "1")
			json.NewEncoder(w).Encode(orders)
		case r.URL.Path == "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "rotated-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": pilotSkills()})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server, func() []int32 {
		mu.Lock()
		defer mu.Unlock()
		return append([]int32(nil), requested...)
	}
}

// historyFilteredOrders is the book-only fixture reused from
// filters_test.go: Morphite (11399) is a two-sided, book-only survivor;
// Tritanium (34) is thin and never reaches the history stage.
func historyFilteredOrders() []map[string]any {
	return []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 24500.0, "is_buy_order": false, "range": "region"},
		{"order_id": 3, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
		{"order_id": 4, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18000.0, "is_buy_order": true, "range": "station"},
		{"order_id": 5, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 200.0, "is_buy_order": false, "range": "region"},
		{"order_id": 6, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 100.0, "is_buy_order": true, "range": "station"},
	}
}

func TestHistoryFilteredUniverseFetchesHistoryOnlyForBookOnlySurvivors(t *testing.T) {
	history := map[int32][]map[string]any{
		// Morphite: 30 days of solid volume, low/high wide of best bid/ask
		// (18220/24080) -- clears all three history filters.
		11399: historyDays(30, 100, 30000, 15000),
		// Tritanium never reaches the history stage -- if it were
		// requested, this (absent) fixture would decode to an empty
		// history and fail min-history, revealing the bug.
	}
	server, historyRequests := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	cfg := testConfig(t, server.URL)

	recs, excluded, _, warnings, err := cli.HistoryFilteredUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("HistoryFilteredUniverse: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}

	if len(recs) != 1 || recs[0].TypeID != 11399 {
		t.Fatalf("got recs=%+v, want only Morphite (11399)", recs)
	}
	// Tritanium is already excluded by the book-only stage (thin book);
	// it must not pick up a second, history-stage exclusion.
	if len(excluded) != 1 || excluded[0].TypeID != 34 {
		t.Fatalf("got excluded=%+v, want exactly one entry for Tritanium (34)", excluded)
	}

	requests := historyRequests()
	if len(requests) != 1 || requests[0] != 11399 {
		t.Fatalf("got history requests %v, want exactly one for Morphite (11399)", requests)
	}
}

func TestHistoryFilteredUniverseDropsCandidatesFailingAHistoryFilter(t *testing.T) {
	history := map[int32][]map[string]any{
		// Only 3 days of history: fails the 7-day minimum.
		11399: historyDays(3, 100, 30000, 15000),
	}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	cfg := testConfig(t, server.URL)

	recs, excluded, _, _, err := cli.HistoryFilteredUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("HistoryFilteredUniverse: %v", err)
	}

	if len(recs) != 0 {
		t.Fatalf("got recs=%+v, want none (Morphite fails min history, Tritanium fails book-only)", recs)
	}
	var reasons []string
	for _, e := range excluded {
		reasons = append(reasons, e.Reason)
	}
	if len(excluded) != 2 {
		t.Fatalf("got excluded=%+v, want two entries (Tritanium book-only, Morphite history)", excluded)
	}
}

func TestHistoryFilteredUniverseCachesHistoryAcrossCalls(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
	}
	server, historyRequests := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	cfg := testConfig(t, server.URL)

	if _, _, _, _, err := cli.HistoryFilteredUniverse(t.Context(), cfg); err != nil {
		t.Fatalf("HistoryFilteredUniverse (first call): %v", err)
	}
	if _, _, _, _, err := cli.HistoryFilteredUniverse(t.Context(), cfg); err != nil {
		t.Fatalf("HistoryFilteredUniverse (second call): %v", err)
	}

	requests := historyRequests()
	if len(requests) != 1 {
		t.Fatalf("got %d history requests across two calls, want 1 (the second call should reuse the cached history)", len(requests))
	}
}
