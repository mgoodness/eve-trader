package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
)

// bookFilteredFixtureServer serves the region-orders feed plus the SSO
// token/skills/standings routes the pilot's live fees are derived from
// (ticket #16), so BookFilteredUniverse can run its pricing-rule stage
// end to end.
func bookFilteredFixtureServer(t *testing.T, orders []map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
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
	return server
}

func TestBookFilteredUniverseAppliesTheBookOnlyFiltersToTheTwoSidedUniverse(t *testing.T) {
	orders := []map[string]any{
		// Morphite: two-sided, wide margin, deep enough on both sides.
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 24500.0, "is_buy_order": false, "range": "region"},
		{"order_id": 3, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
		{"order_id": 4, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18000.0, "is_buy_order": true, "range": "station"},
		// Tritanium: two-sided, but thin (one order per side) -- excluded.
		{"order_id": 5, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 200.0, "is_buy_order": false, "range": "region"},
		{"order_id": 6, "type_id": 34, "location_id": 60004588, "system_id": 30002510, "volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": 100.0, "is_buy_order": true, "range": "station"},
	}
	server := bookFilteredFixtureServer(t, orders)
	cfg := testConfig(t, server.URL)

	recs, excluded, _, warnings, err := cli.BookFilteredUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BookFilteredUniverse: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}

	if len(recs) != 1 || recs[0].TypeID != 11399 {
		t.Fatalf("got recs=%+v, want only Morphite (11399)", recs)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 34 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for Tritanium (34) with a non-empty reason (thin book)", excluded)
	}
}
