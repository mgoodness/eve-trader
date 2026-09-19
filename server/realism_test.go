package server_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

// renderIndex starts a server over sqlDB and returns the rendered "/" body.
func renderIndex(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}
	return body
}

// TestRealismFiltersHideItemsFailingEachRule seeds a passing item alongside
// an item that breaks exactly one always-on realism rule, and asserts the
// failing item is absent from the rendered table (not merely togglable),
// the passing item is present, and the hidden count is accurate.
func TestRealismFiltersHideItemsFailingEachRule(t *testing.T) {
	cases := []struct {
		rule   string
		absent string
		seed   func(t *testing.T, sqlDB *sql.DB)
	}{
		{"thin history", "Thin History Ore", func(t *testing.T, sqlDB *sql.DB) {
			dbtest.SeedItem(t, sqlDB, 50, "Thin History Ore")
			dbtest.SeedBook(t, sqlDB, 50, 100, 120)
			dbtest.SeedHistory(t, sqlDB, 50, 40, 40, 40) // 3 trade-days < 7
		}},
		{"manipulated history", "Manipulated Ore", func(t *testing.T, sqlDB *sql.DB) {
			dbtest.SeedItem(t, sqlDB, 51, "Manipulated Ore")
			dbtest.SeedBook(t, sqlDB, 51, 100, 120)
			days := make([]dbtest.HistoryDay, 10) // 7 <= trade-days < 14
			for i := range days {
				days[i] = dbtest.HistoryDay{Volume: 40, OrderCount: 5, Average: 10, Highest: 10, Lowest: 10}
			}
			days[0].Highest = 100 // swing = 100/1 = 100x > 20x
			days[1].Lowest = 1
			dbtest.SeedHistoryDays(t, sqlDB, 51, days...)
		}},
		{"single-order spread", "Single Order Ore", func(t *testing.T, sqlDB *sql.DB) {
			dbtest.SeedItem(t, sqlDB, 52, "Single Order Ore")
			dbtest.SeedOrder(t, sqlDB, 520, 52, true, 100)
			dbtest.SeedOrder(t, sqlDB, 521, 52, true, 90) // outside the 5% buy band
			dbtest.SeedOrder(t, sqlDB, 522, 52, false, 120)
			dbtest.SeedOrder(t, sqlDB, 523, 52, false, 121)
			dbtest.SeedHistory(t, sqlDB, 52, 40, 40, 40, 40, 40, 40, 40)
		}},
		{"incomplete history", "Unpriced Ore", func(t *testing.T, sqlDB *sql.DB) {
			dbtest.SeedItem(t, sqlDB, 53, "Unpriced Ore")
			dbtest.SeedBook(t, sqlDB, 53, 100, 120)
			days := make([]dbtest.HistoryDay, 7)
			for i := range days {
				days[i] = dbtest.HistoryDay{Volume: 40, OrderCount: 5, NullPrices: true}
			}
			dbtest.SeedHistoryDays(t, sqlDB, 53, days...)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			sqlDB := dbtest.OpenDB(t)
			dbtest.SeedSkills(t, sqlDB, 1, 4, 3)
			dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")
			seedCandidate(t, sqlDB, 34, "Passing Ore", 100, 120, 40)
			tc.seed(t, sqlDB)

			body := renderIndex(t, sqlDB)
			if strings.Contains(body, tc.absent) {
				t.Errorf("GET / body contains %q, want absent; body:\n%s", tc.absent, body)
			}
			if !strings.Contains(body, "Passing Ore") {
				t.Errorf("GET / body missing passing item; body:\n%s", body)
			}
			if !strings.Contains(body, "Hidden by realism filters: <strong>1</strong>") {
				t.Errorf("GET / body missing hidden count of 1; body:\n%s", body)
			}
		})
	}
}

// TestRealismCardNamesRulesAndFootnoteDiscloses asserts the sidebar card
// names the active always-on rules and that the footnote discloses the
// automatic thin/manipulated exclusion. It also checks the sidebar layout:
// the realism card precedes the opportunity table.
func TestRealismCardNamesRulesAndFootnoteDiscloses(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")
	seedCandidate(t, sqlDB, 34, "Passing Ore", 100, 120, 40)

	body := renderIndex(t, sqlDB)

	for _, want := range []string{
		"Always-on realism filters",
		"At least 7 days of recent trade history",
		"No manipulated history",
		"No single-order spreads",
		"Complete price history",
		"thin or manipulated trade history",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing %q; body:\n%s", want, body)
		}
	}

	cardIdx := strings.Index(body, "Always-on realism filters")
	tableIdx := strings.Index(body, "<table>")
	if cardIdx == -1 || tableIdx == -1 || cardIdx > tableIdx {
		t.Errorf("realism card should precede the opportunity table; card=%d table=%d", cardIdx, tableIdx)
	}
}
