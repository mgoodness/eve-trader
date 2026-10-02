package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// bookFilteredFixtureServer serves the region-orders feed plus the SSO
// token/skills/standings routes the pilot's live fees are derived from
// (ticket #16), so BookFilteredUniverse can run its pricing-rule stage
// end to end. ownOrders serves the character's open orders (ticket #51),
// so a test can put the pilot's own order among orders and assert it is
// excluded from the effective books; pass nil for none.
func bookFilteredFixtureServer(t *testing.T, orders []map[string]any, ownOrders []map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/orders/"):
			json.NewEncoder(w).Encode(ownOrders)
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
	server := bookFilteredFixtureServer(t, orders, nil)
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

// TestBookFilteredUniverseExcludesThePilotsOwnOpenOrderFromPricing pins
// ticket #51: the pilot's own open orders never count as a competing bid
// or ask. Type 40's only buy order is the pilot's own -- once excluded, it
// has no buy side and drops out of the candidate universe like any other
// one-sided type. Morphite's best bid is also the pilot's own order, but a
// stranger's competing bid is still present, so Morphite survives, priced
// against the stranger's order, not the pilot's.
func TestBookFilteredUniverseExcludesThePilotsOwnOpenOrderFromPricing(t *testing.T) {
	ownOrders := []map[string]any{
		openOrderMap(3, 11399, true, 18220, 5), // Morphite: pilot's own best bid.
		openOrderMap(9, 40, true, 1000, 5),     // Type 40: pilot's only buy order.
	}
	orders := []map[string]any{
		// Morphite: two-sided. The pilot's own order (3) is the best bid;
		// two strangers' orders (4, 11) remain once it's excluded, so the
		// buy side still clears the thin-book minimum (2).
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 24500.0, "is_buy_order": false, "range": "region"},
		{"order_id": 3, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
		{"order_id": 4, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18000.0, "is_buy_order": true, "range": "station"},
		{"order_id": 11, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 17900.0, "is_buy_order": true, "range": "station"},
		// Type 40: two-sided book, but its only buy order (9) is the
		// pilot's own -- once excluded, it has no buy side at all.
		{"order_id": 7, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2000.0, "is_buy_order": false, "range": "region"},
		{"order_id": 8, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2100.0, "is_buy_order": false, "range": "region"},
		{"order_id": 9, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 1000.0, "is_buy_order": true, "range": "station"},
	}
	server := bookFilteredFixtureServer(t, orders, ownOrders)
	cfg := testConfig(t, server.URL)

	recs, excluded, _, warnings, err := cli.BookFilteredUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BookFilteredUniverse: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}

	// Type 40 never reaches the two-sided stage: its only buy order was the
	// pilot's own, so it silently drops out like any other one-sided type
	// (no tracked Excluded reason, spec'd out of scope by ticket #51).
	for _, e := range excluded {
		if e.TypeID == 40 {
			t.Errorf("got an Excluded entry for type 40, want it to drop silently (one-sided, not filtered)")
		}
	}
	for _, r := range recs {
		if r.TypeID == 40 {
			t.Errorf("got a recommendation for type 40, want none (its only buy order was the pilot's own)")
		}
	}

	// Morphite survives, priced against the stranger's best bid (18000), not
	// the pilot's own order's price (18220).
	var morphite *engine.BuyRecommendation
	for i := range recs {
		if recs[i].TypeID == 11399 {
			morphite = &recs[i]
		}
	}
	if morphite == nil {
		t.Fatalf("got recs=%+v, want a Morphite (11399) recommendation priced against the stranger's competing bid", recs)
	}
	if morphite.BuyPrice >= 18220 {
		t.Errorf("got Morphite BuyPrice=%v, want it priced off the stranger's bid (18000) + delta, not the pilot's own order's price (18220)", morphite.BuyPrice)
	}
}

// TestBookFilteredUniverseExcludesACorpWalletOrderTheSameAsAPersonalOne pins
// ticket #51's acceptance criterion that a corp-wallet order
// (IsCorporation: true) is excluded the same as a personal one: it is
// still an order this character placed, and v1 has no multi-character/corp
// distinction to make excluding only one of them meaningful.
func TestBookFilteredUniverseExcludesACorpWalletOrderTheSameAsAPersonalOne(t *testing.T) {
	ownOrders := []map[string]any{
		{"order_id": 9, "type_id": 40, "location_id": 60004588, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 1000.0, "is_buy_order": true, "is_corporation": true, "range": "station"},
	}
	orders := []map[string]any{
		// Type 40's only buy order is the pilot's own corp-wallet order (9)
		// -- once excluded, it has no buy side at all.
		{"order_id": 7, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2000.0, "is_buy_order": false, "range": "region"},
		{"order_id": 8, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2100.0, "is_buy_order": false, "range": "region"},
		{"order_id": 9, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 1000.0, "is_buy_order": true, "range": "station"},
	}
	server := bookFilteredFixtureServer(t, orders, ownOrders)
	cfg := testConfig(t, server.URL)

	recs, _, _, _, err := cli.BookFilteredUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BookFilteredUniverse: %v", err)
	}
	for _, r := range recs {
		if r.TypeID == 40 {
			t.Errorf("got a recommendation for type 40, want none (its only buy order was the pilot's own corp-wallet order)")
		}
	}
}
