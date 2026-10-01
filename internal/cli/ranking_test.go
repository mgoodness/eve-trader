package cli_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
)

// rankedFilteredOrders extends historyFilteredOrders with a second,
// higher-EDP survivor (type 40): a tighter book, wider profit per unit, and
// a much higher 30-day ADV than Morphite (11399), so a test can tell the
// two apart by rank.
func rankedFilteredOrders() []map[string]any {
	orders := historyFilteredOrders()
	return append(orders,
		map[string]any{"order_id": 7, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2000.0, "is_buy_order": false, "range": "region"},
		map[string]any{"order_id": 8, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 2100.0, "is_buy_order": false, "range": "region"},
		map[string]any{"order_id": 9, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 1000.0, "is_buy_order": true, "range": "station"},
		map[string]any{"order_id": 10, "type_id": 40, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 950.0, "is_buy_order": true, "range": "station"},
	)
}

func TestRankedUniverseSortsSurvivorsByExpectedDailyProfitDescending(t *testing.T) {
	history := map[int32][]map[string]any{
		// Morphite: profit/unit \u2248 3693.605, ADV 100 -> EDP \u2248 73,872 at the
		// default 20% capture rate.
		11399: historyDays(30, 100, 30000, 15000),
		// Type 40: profit/unit \u2248 504.525 (\u2248 650.525 before the 100 ISK per-order
		// broker floor binds on both legs), ADV 1000 -> EDP \u2248 100,905 -- still
		// higher than Morphite despite the thinner per-unit profit, because of
		// the much larger volume.
		40: historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)

	recs, excluded, _, _, err := cli.RankedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("RankedUniverse: %v", err)
	}
	// Tritanium (34) is still excluded by the book-only stage.
	if len(excluded) != 1 || excluded[0].TypeID != 34 {
		t.Fatalf("got excluded=%+v, want exactly one entry for Tritanium (34)", excluded)
	}

	if len(recs) != 2 {
		t.Fatalf("got recs=%+v, want two survivors", recs)
	}
	if recs[0].TypeID != 40 || recs[1].TypeID != 11399 {
		t.Fatalf("got order %d, %d; want type 40 ranked ahead of Morphite (11399) by expected daily profit", recs[0].TypeID, recs[1].TypeID)
	}

	wantEDPHigh := 504.525 * 0.20 * 1000.0
	wantEDPLow := 3693.605 * 0.20 * 100.0
	if math.Abs(recs[0].ExpectedDailyProfit-wantEDPHigh) > 1e-3 {
		t.Errorf("got type 40 ExpectedDailyProfit=%v, want %v", recs[0].ExpectedDailyProfit, wantEDPHigh)
	}
	if math.Abs(recs[1].ExpectedDailyProfit-wantEDPLow) > 1e-3 {
		t.Errorf("got Morphite ExpectedDailyProfit=%v, want %v", recs[1].ExpectedDailyProfit, wantEDPLow)
	}
}
