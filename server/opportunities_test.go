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

// seedCandidate seeds an item, a priced 7-trade-day history at the given
// daily volume, and a two-orders-per-side book, so the item clears every
// always-on realism filter.
func seedCandidate(t *testing.T, sqlDB *sql.DB, typeID int, name string, buy, sell, volume float64) {
	t.Helper()
	dbtest.SeedItem(t, sqlDB, typeID, name)
	dbtest.SeedBook(t, sqlDB, typeID, buy, sell)
	volumes := make([]int, 7)
	for i := range volumes {
		volumes[i] = int(volume)
	}
	dbtest.SeedHistory(t, sqlDB, typeID, volumes...)
}

// seedOpportunityFixtures seeds the same four items used throughout: two
// above the user-filter defaults (Tritanium ranks higher than Pyerite by
// ISK/day), and two below the defaults (one on margin, one on volume). All
// four clear the always-on realism filters.
func seedOpportunityFixtures(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3) // Broker Relations 4, Accounting 3
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")

	// avg 40. Deliberately not a volume that lands the resulting ISK/day
	// exactly on a rounding half-boundary (e.g. avg 50 -> exactly 500.5),
	// which is sensitive to floating-point-arithmetic-order differences
	// across platforms/compilers and made this fixture flaky in CI.
	seedCandidate(t, sqlDB, 34, "Tritanium", 100, 120, 40)

	seedCandidate(t, sqlDB, 35, "Pyerite", 50, 60, 30) // avg 30

	seedCandidate(t, sqlDB, 36, "Below Margin Ore", 100, 103, 100) // margin 2.9% < 5%

	seedCandidate(t, sqlDB, 37, "Below Volume Ore", 200, 240, 5) // avg 5 < 10 units/day
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

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}

	// Column headers match the spec: Item, Buy, Sell, Net margin, ISK/unit,
	// Vol/day, ISK/day.
	for _, want := range []string{">Item<", ">Buy<", ">Sell<", ">Net margin<", ">ISK/unit<", ">Vol/day<", ">ISK/day<"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing column header %q", want)
		}
	}

	// Default rank is ISK/day descending: Tritanium (≈80 ISK/day at the
	// 20% capture rate) before Pyerite (≈30 ISK/day).
	tritaniumIdx := strings.Index(body, "Tritanium")
	pyeriteIdx := strings.Index(body, "Pyerite")
	if tritaniumIdx == -1 || pyeriteIdx == -1 {
		t.Fatalf("GET / body missing above-threshold items; body:\n%s", body)
	}
	if tritaniumIdx > pyeriteIdx {
		t.Errorf("GET / rendered Pyerite before Tritanium, want ISK/day descending (Tritanium first)")
	}

	// Values are computed via the real formula (not hardcoded): R_b=1.8%,
	// R_t=5.025% at Broker Relations 4 / Accounting 3. π≈10.01; net margin
	// = π/sell = 8.3%; ISK/day = π × 40 × 0.20 ≈ 80. Pyerite's net margin
	// is also 8.3%, and its capture-scaled ISK/day ≈ 30.
	for _, want := range []string{"100 ISK", "120 ISK", "8.3%", "10 ISK", "80 ISK", "30 ISK"} {
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

	// Persistent header footnote states both the fee/capture assumption and
	// the Heimatar-region-volume approximation caveat.
	for _, want := range []string{"20%", "Volume figures are region-wide"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing footnote disclosure %q", want)
		}
	}
}

// TestIndexOmitsAllRowsBelowThresholdWithNoQualifyingItems asserts an
// empty (but non-erroring) table renders when every seeded item is below
// threshold.
func TestIndexOmitsAllRowsBelowThresholdWithNoQualifyingItems(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")
	seedCandidate(t, sqlDB, 36, "Below Margin Ore", 100, 103, 100)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
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

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	cases := []struct {
		sort       string
		wantFirst  string
		wantSecond string
	}{
		{"buy", "Pyerite", "Tritanium"},     // Buy 50 < 100
		{"sell", "Pyerite", "Tritanium"},    // Sell 60 < 120
		{"margin", "Tritanium", "Pyerite"},  // both net ≈8.3%, stable order preserved
		{"iskunit", "Tritanium", "Pyerite"}, // profit/unit 10.01 > 5.005
		{"volday", "Tritanium", "Pyerite"},  // 40 > 30
		{"iskday", "Tritanium", "Pyerite"},  // ≈80 > ≈30 (default rank)
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

// TestIndexIncludesStationOwnerStandings renders the same spread with and
// without the Rens owner's standings and asserts the fee-adjusted figures
// rise. It is the end-to-end proof that the ranking uses the standings rate
// and that the footnote is truthful about it (docs/spec/v2.md §9).
func TestIndexIncludesStationOwnerStandings(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedOpportunityFixtures(t, sqlDB)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	// Skills-only baseline: R_b = 1.8%, so π ≈ 10.01 → "10 ISK"/unit.
	_, before := getBody(t, srv.URL+"/")
	if !strings.Contains(before, "10 ISK") {
		t.Fatalf("baseline render missing skills-only ISK/unit; body:\n%s", before)
	}
	if !strings.Contains(before, "standings toward the Rens station owner") {
		t.Errorf("GET / body missing footnote standings disclosure")
	}

	// Rens owner Brutor Tribe (1000049) and faction Minmatar Republic
	// (500002), each at +10, lower R_b to 1.3%: π ≈ 11.11 → "11 ISK"/unit.
	dbtest.SeedStationOwner(t, sqlDB, 60004588, 1000049)
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", 10)
	dbtest.SeedStanding(t, sqlDB, 1, 500002, "faction", 10)

	_, after := getBody(t, srv.URL+"/")
	if !strings.Contains(after, "11 ISK") {
		t.Fatalf("standings render missing standings-adjusted ISK/unit; body:\n%s", after)
	}
}
