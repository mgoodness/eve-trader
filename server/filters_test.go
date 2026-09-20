package server_test

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

// newTestServer starts the app over sqlDB and returns its base URL.
func newTestServer(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	t.Cleanup(srv.Close)
	return srv.URL
}

// seedFilterFixtures seeds six realistic candidates that exercise every
// user-filter control at its default and at a cleared/overridden value:
//
//	item             volume  gross margin  sell
//	Tritanium          40      16.7%        120
//	Pyerite            30      16.7%         60
//	Thin Margin Ore   100       2.9%        103
//	Low Volume Ore      5      16.7%        240
//	Wide Margin Ore    25      66.7%        300
//	Pricey Ore         25      16.7%       1200
//
// At the defaults (min volume 20, gross margin 7-60%, no sell cap) the
// first, second, and sixth show; the other three are outside the filters.
// Every candidate clears the always-on realism filters.
func seedFilterFixtures(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")

	seedCandidate(t, sqlDB, 34, "Tritanium", 100, 120, 40)
	seedCandidate(t, sqlDB, 35, "Pyerite", 50, 60, 30)
	seedCandidate(t, sqlDB, 36, "Thin Margin Ore", 100, 103, 100)
	seedCandidate(t, sqlDB, 37, "Low Volume Ore", 200, 240, 5)
	seedCandidate(t, sqlDB, 38, "Wide Margin Ore", 100, 300, 25)
	seedCandidate(t, sqlDB, 39, "Pricey Ore", 1000, 1200, 25)
}

// inputValue is the exact "name=... value=..." pair the filter form renders
// for one control, so a test asserts the displayed control value rather than
// an incidental substring.
func inputValue(name, value string) string {
	return `name="` + name + `" value="` + value + `"`
}

func TestFilterFormRendersControlsDefaultsAndTooltips(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedFilterFixtures(t, sqlDB)

	body := renderIndex(t, sqlDB)

	for _, want := range []string{
		inputValue("minvol", "20"),
		inputValue("minmargin", "7"),
		inputValue("maxmargin", "60"),
		inputValue("maxsell", ""),
		`name="sort" value="iskday"`,
		`href="/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing %q; body:\n%s", want, body)
		}
	}

	// Each control explains itself on hover (title + help affordance).
	for _, want := range []string{
		"average daily Heimatar volume is below this",
		"gross margin is below this",
		"gross margin is above this",
		"Blank means no cap",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / body missing tooltip text %q", want)
		}
	}
}

// TestFilterParamMatrix drives the stateless URL contract for every control:
// absent uses the default, present-but-empty removes the bound, a present
// value is used, and an invalid/out-of-range value (or min margin greater
// than max margin) falls back to the defaults -- all without an error page.
func TestFilterParamMatrix(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		inputs map[string]string
		shown  []string
		absent []string
	}{
		{
			name:   "absent params use defaults",
			query:  "/",
			inputs: map[string]string{"minvol": "20", "minmargin": "7", "maxmargin": "60", "maxsell": ""},
			shown:  []string{"Tritanium", "Pyerite", "Pricey Ore"},
			absent: []string{"Thin Margin Ore", "Low Volume Ore", "Wide Margin Ore"},
		},
		{
			name:   "empty minvol removes the floor",
			query:  "/?minvol=",
			inputs: map[string]string{"minvol": ""},
			shown:  []string{"Low Volume Ore"},
			absent: []string{"Thin Margin Ore", "Wide Margin Ore"},
		},
		{
			name:   "value minvol is used",
			query:  "/?minvol=35",
			inputs: map[string]string{"minvol": "35"},
			shown:  []string{"Tritanium"},
			absent: []string{"Pyerite", "Pricey Ore", "Low Volume Ore"},
		},
		{
			name:   "empty minmargin removes the floor",
			query:  "/?minmargin=",
			inputs: map[string]string{"minmargin": ""},
			shown:  []string{"Thin Margin Ore", "Tritanium"},
			absent: []string{"Wide Margin Ore"},
		},
		{
			name:   "empty maxmargin removes the cap",
			query:  "/?maxmargin=",
			inputs: map[string]string{"maxmargin": ""},
			shown:  []string{"Wide Margin Ore", "Tritanium"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "empty maxsell removes the cap",
			query:  "/?maxsell=",
			inputs: map[string]string{"maxsell": ""},
			shown:  []string{"Pricey Ore"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "value minmargin is used",
			query:  "/?minmargin=2",
			inputs: map[string]string{"minmargin": "2"},
			shown:  []string{"Thin Margin Ore", "Tritanium"},
			absent: []string{"Wide Margin Ore"},
		},
		{
			name:   "value maxmargin is used",
			query:  "/?maxmargin=70",
			inputs: map[string]string{"maxmargin": "70"},
			shown:  []string{"Wide Margin Ore", "Tritanium"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "value maxsell is used",
			query:  "/?maxsell=200",
			inputs: map[string]string{"maxsell": "200"},
			shown:  []string{"Tritanium", "Pyerite"},
			absent: []string{"Pricey Ore", "Low Volume Ore"},
		},
		{
			name:   "non-numeric minvol falls back to default",
			query:  "/?minvol=abc",
			inputs: map[string]string{"minvol": "20"},
			shown:  []string{"Tritanium"},
			absent: []string{"Low Volume Ore"},
		},
		{
			name:   "out-of-range minmargin falls back to default",
			query:  "/?minmargin=999",
			inputs: map[string]string{"minmargin": "7"},
			shown:  []string{"Tritanium"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "out-of-range maxmargin falls back to default",
			query:  "/?maxmargin=-1",
			inputs: map[string]string{"maxmargin": "60"},
			shown:  []string{"Tritanium"},
			absent: []string{"Wide Margin Ore"},
		},
		{
			name:   "negative maxsell falls back to blank default",
			query:  "/?maxsell=-5",
			inputs: map[string]string{"maxsell": ""},
			shown:  []string{"Pricey Ore"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "non-numeric minmargin falls back to default",
			query:  "/?minmargin=xyz",
			inputs: map[string]string{"minmargin": "7"},
			shown:  []string{"Tritanium"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "non-numeric maxmargin falls back to default",
			query:  "/?maxmargin=xyz",
			inputs: map[string]string{"maxmargin": "60"},
			shown:  []string{"Tritanium"},
			absent: []string{"Wide Margin Ore"},
		},
		{
			name:   "non-numeric maxsell falls back to blank default",
			query:  "/?maxsell=xyz",
			inputs: map[string]string{"maxsell": ""},
			shown:  []string{"Pricey Ore"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "infinite minvol falls back to default",
			query:  "/?minvol=Inf",
			inputs: map[string]string{"minvol": "20"},
			shown:  []string{"Tritanium"},
			absent: []string{"Low Volume Ore"},
		},
		{
			name:   "NaN minmargin falls back to default",
			query:  "/?minmargin=NaN",
			inputs: map[string]string{"minmargin": "7"},
			shown:  []string{"Tritanium"},
			absent: []string{"Thin Margin Ore"},
		},
		{
			name:   "min margin over max margin resets both",
			query:  "/?minmargin=80&maxmargin=50",
			inputs: map[string]string{"minmargin": "7", "maxmargin": "60"},
			shown:  []string{"Tritanium"},
			absent: []string{"Thin Margin Ore", "Wide Margin Ore"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB := dbtest.OpenDB(t)
			seedFilterFixtures(t, sqlDB)

			srv := newTestServer(t, sqlDB)
			status, got := getBody(t, srv+tc.query)
			if status != 200 {
				t.Fatalf("GET %s status = %d, want 200", tc.query, status)
			}
			body := got

			for name, value := range tc.inputs {
				if !strings.Contains(body, inputValue(name, value)) {
					t.Errorf("GET %s: control %s not rendered as %q; body:\n%s", tc.query, name, value, body)
				}
			}
			for _, name := range tc.shown {
				if !strings.Contains(body, name) {
					t.Errorf("GET %s: expected %q to be shown; body:\n%s", tc.query, name, body)
				}
			}
			for _, name := range tc.absent {
				if strings.Contains(body, name) {
					t.Errorf("GET %s: expected %q to be excluded; body:\n%s", tc.query, name, body)
				}
			}
		})
	}
}

// TestFilterAndSortCompose asserts the one-query-string contract: the filter
// form carries the active sort, sortable headers carry the active filters,
// and the htmx partial re-renders sorted rows without dropping either.
func TestFilterAndSortCompose(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedFilterFixtures(t, sqlDB)

	srv := newTestServer(t, sqlDB)

	// The page for a filtered, sorted URL carries both into the controls and
	// every sortable header link.
	_, page := getBody(t, srv+"/?sort=buy&minvol=25")
	if !strings.Contains(page, `name="sort" value="buy"`) {
		t.Errorf("filtered page did not carry the active sort into the form")
	}
	if !strings.Contains(page, `minvol=25&amp;sort=sell`) {
		t.Errorf("sortable header did not carry the active filters; body:\n%s", page)
	}
	if !strings.Contains(page, `hx-push-url="/?minvol=25&amp;sort=sell"`) {
		t.Errorf("sortable header missing canonical push URL")
	}

	// Sorting the filtered set preserves the filters and reorders the rows.
	_, buySorted := getBody(t, srv+"/opportunities?sort=buy&minvol=25")
	pyerite := strings.Index(buySorted, "Pyerite")
	tritanium := strings.Index(buySorted, "Tritanium")
	pricey := strings.Index(buySorted, "Pricey Ore")
	if pyerite == -1 || tritanium == -1 || pricey == -1 {
		t.Fatalf("sorted partial missing expected rows; body:\n%s", buySorted)
	}
	if !(pyerite < tritanium && tritanium < pricey) {
		t.Errorf("sort=buy order wrong: want Pyerite, Tritanium, Pricey Ore")
	}
	if strings.Contains(buySorted, "Thin Margin Ore") {
		t.Errorf("sorting dropped the active min-margin filter; body:\n%s", buySorted)
	}

	_, volSorted := getBody(t, srv+"/opportunities?sort=volday&minvol=25")
	if strings.Index(volSorted, "Tritanium") > strings.Index(volSorted, "Pyerite") {
		t.Errorf("sort=volday did not reorder within the filtered set")
	}
}

// TestSummaryAndEmptyStateAreDynamic asserts the summary line and empty
// state reflect the shown/hidden counts and the active controls rather than
// any hardcoded v1 threshold.
func TestSummaryAndEmptyStateAreDynamic(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedFilterFixtures(t, sqlDB)
	srv := newTestServer(t, sqlDB)

	_, body := getBody(t, srv+"/")
	for _, want := range []string{
		"Showing <strong>3</strong> of 6 items",
		"<strong>0</strong> hidden by realism filters",
		"<strong>3</strong> outside your filters",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / summary missing %q; body:\n%s", want, body)
		}
	}
	for _, stale := range []string{"10 units/day", "5% min margin", "v1 thresholds"} {
		if strings.Contains(body, stale) {
			t.Errorf("GET / body still hardcodes old threshold copy %q", stale)
		}
	}

	// The empty state names the active controls and the live counts.
	_, empty := getBody(t, srv+"/?minvol=100000")
	for _, want := range []string{
		"No opportunities match your current filters",
		"minimum daily volume 100000",
		"Showing <strong>0</strong> of 6 items",
		"<strong>6</strong> outside your filters",
	} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty state missing %q; body:\n%s", want, empty)
		}
	}
}
