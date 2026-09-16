package server_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

// seedOpportunityFixtures seeds the same four items used throughout: two
// above the v1 filter thresholds (Tritanium ranks higher than Pyerite by
// ISK/day), and two below threshold (one on margin, one on volume).
func seedOpportunityFixtures(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3) // Broker Relations 4, Accounting 3

	dbtest.SeedItem(t, sqlDB, 34, "Tritanium")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, false, 120)
	// avg 40. Deliberately not a volume that lands the resulting ISK/day
	// exactly on a rounding half-boundary (e.g. avg 50 -> exactly 500.5),
	// which is sensitive to floating-point-arithmetic-order differences
	// across platforms/compilers and made this fixture flaky in CI.
	dbtest.SeedHistory(t, sqlDB, 34, 38, 42)

	dbtest.SeedItem(t, sqlDB, 35, "Pyerite")
	dbtest.SeedOrder(t, sqlDB, 3, 35, true, 50)
	dbtest.SeedOrder(t, sqlDB, 4, 35, false, 60)
	dbtest.SeedHistory(t, sqlDB, 35, 20, 40) // avg 30

	dbtest.SeedItem(t, sqlDB, 36, "Below Margin Ore")
	dbtest.SeedOrder(t, sqlDB, 5, 36, true, 100)
	dbtest.SeedOrder(t, sqlDB, 6, 36, false, 103) // margin 2.9% < 5%
	dbtest.SeedHistory(t, sqlDB, 36, 100, 100)

	dbtest.SeedItem(t, sqlDB, 37, "Below Volume Ore")
	dbtest.SeedOrder(t, sqlDB, 7, 37, true, 200)
	dbtest.SeedOrder(t, sqlDB, 8, 37, false, 240) // healthy margin
	dbtest.SeedHistory(t, sqlDB, 37, 5, 5)        // avg 5 < 10 units/day
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestIndexRendersRankedOpportunityTable is the ticket's headline
// acceptance test: seeded market_order/market_history/item_type/
// character_skill rows render as one row per above-threshold opportunity,
// with the real formula's computed values, ranked ISK/day descending by
// default, and below-threshold items absent.
func TestIndexRendersRankedOpportunityTable(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedOpportunityFixtures(t, sqlDB)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}

	// Column headers match the spec: Item, Buy, Sell, Margin, ISK/unit,
	// Vol/day, ISK/day.
	for _, want := range []string{">Item<", ">Buy<", ">Sell<", ">Margin<", ">ISK/unit<", ">Vol/day<", ">ISK/day<"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing column header %q", want)
		}
	}

	// Default rank is ISK/day descending: Tritanium (≈400.4 ISK/day) before
	// Pyerite (≈150.15 ISK/day).
	tritaniumIdx := strings.Index(body, "Tritanium")
	pyeriteIdx := strings.Index(body, "Pyerite")
	if tritaniumIdx == -1 || pyeriteIdx == -1 {
		t.Fatalf("GET / body missing above-threshold items; body:\n%s", body)
	}
	if tritaniumIdx > pyeriteIdx {
		t.Errorf("GET / rendered Pyerite before Tritanium, want ISK/day descending (Tritanium first)")
	}

	// Values are computed via the real formula (not hardcoded): R_b=1.8%,
	// R_t=5.025% at Broker Relations 4 / Accounting 3. π≈10.01, M=16.7%,
	// ISK/day≈400.4, rounds to 400.
	for _, want := range []string{"100 ISK", "120 ISK", "16.7%", "10 ISK", "400 ISK"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing computed value %q; body:\n%s", want, body)
		}
	}

	// Below-threshold items are absent entirely.
	for _, absent := range []string{"Below Margin Ore", "Below Volume Ore"} {
		if strings.Contains(body, absent) {
			t.Errorf("GET / body contains below-threshold item %q, want absent", absent)
		}
	}

	// Persistent header footnote states the Heimatar-region-volume
	// approximation caveat.
	if !strings.Contains(body, "Heimatar-region-wide") {
		t.Errorf("GET / body missing Heimatar-region-volume footnote")
	}
}

// TestIndexOmitsAllRowsBelowThresholdWithNoQualifyingItems asserts an
// empty (but non-erroring) table renders when every seeded item is below
// threshold.
func TestIndexOmitsAllRowsBelowThresholdWithNoQualifyingItems(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0)
	dbtest.SeedItem(t, sqlDB, 36, "Below Margin Ore")
	dbtest.SeedOrder(t, sqlDB, 1, 36, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 36, false, 103)
	dbtest.SeedHistory(t, sqlDB, 36, 100, 100)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}
	if strings.Contains(body, "Below Margin Ore") {
		t.Errorf("GET / body contains below-threshold item, want absent")
	}
}

// TestOpportunitiesPartialResortsByColumn drives the htmx sort partial for
// each column and asserts the row order changes accordingly, matching the
// winning "dense sortable table" prototype variant's per-header re-sort
// behavior.
func TestOpportunitiesPartialResortsByColumn(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedOpportunityFixtures(t, sqlDB)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB))
	defer srv.Close()

	cases := []struct {
		sort       string
		wantFirst  string
		wantSecond string
	}{
		{"buy", "Pyerite", "Tritanium"},     // Buy 50 < 100
		{"sell", "Pyerite", "Tritanium"},    // Sell 60 < 120
		{"margin", "Tritanium", "Pyerite"},  // both ~16.7%, stable order preserved
		{"iskunit", "Tritanium", "Pyerite"}, // profit/unit 10.01 > 5.005
		{"volday", "Tritanium", "Pyerite"},  // 40 > 30
		{"iskday", "Tritanium", "Pyerite"},  // ≈400.4 > ≈150.15 (default rank)
	}
	for _, tc := range cases {
		t.Run(tc.sort, func(t *testing.T) {
			status, body := getBody(t, srv.URL+"/opportunities?sort="+tc.sort)
			if status != http.StatusOK {
				t.Fatalf("GET /opportunities?sort=%s status = %d, want %d", tc.sort, status, http.StatusOK)
			}
			firstIdx := strings.Index(body, tc.wantFirst)
			secondIdx := strings.Index(body, tc.wantSecond)
			if firstIdx == -1 || secondIdx == -1 {
				t.Fatalf("GET /opportunities?sort=%s missing expected rows; body:\n%s", tc.sort, body)
			}
			if firstIdx > secondIdx {
				t.Errorf("GET /opportunities?sort=%s rendered %s before %s, want %s first", tc.sort, tc.wantSecond, tc.wantFirst, tc.wantFirst)
			}
		})
	}
}
