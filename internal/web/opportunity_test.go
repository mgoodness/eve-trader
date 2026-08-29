package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

func TestParseHubParam(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"", "Jita"},
		{"?hub=Jita", "Jita"},
		{"?hub=Rens", "Rens"},
		{"?hub=Amarr", "Jita"}, // unrecognized falls back to Jita
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/"+c.query, nil)
		if got := parseHubParam(req); got.Name != c.want {
			t.Errorf("parseHubParam(%q) = %q, want %q", c.query, got.Name, c.want)
		}
	}
}

func TestParseFilterParams(t *testing.T) {
	cases := []struct {
		query         string
		wantMinVolume float64
		wantMinMargin float64
		wantMinMarkup float64
	}{
		{"", 0, 0, 0},
		{"?minVolume=1000&minMargin=5.5", 1000, 5.5, 0},
		{"?minVolume=not-a-number", 0, 0, 0}, // invalid falls back to 0
		// minMarkup is typed as a percentage (e.g. 15 for 15%) but
		// scanner.Filter.MinMarkup is a fraction (see CONTEXT.md's
		// Markup definition) -- must convert on the way in.
		{"?minMarkup=15", 0, 0, 0.15},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/"+c.query, nil)
		got := parseFilterParams(req)
		if got.MinVolume != c.wantMinVolume || got.MinMargin != c.wantMinMargin || got.MinMarkup != c.wantMinMarkup {
			t.Errorf("parseFilterParams(%q) = %+v, want {%v %v %v}", c.query, got, c.wantMinVolume, c.wantMinMargin, c.wantMinMarkup)
		}
	}
}

// TestDashboardRendersOpportunityPanel drives the full handler stack --
// HTTP routing, hub-tab/filter query params, the fake ESIClient, and a
// real temp-file SQLite store -- proving the Opportunity Scanner panel
// renders ranked results end-to-end.
func TestDashboardRendersOpportunityPanel(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.TokenResp.AccessToken = fakeCharacterAccessToken(t, 95465499)
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 100, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 5.40},
		{OrderID: 101, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: true, Price: 4.50},
	}
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 500000}}
	srv := newTestServer(t, fake)
	authenticate(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?hub=Jita&minMargin=0.01", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{"Tritanium", "5.40", "4.50", "500,000"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `hub-tab active">Jita`) {
		t.Error("Jita tab should be marked active")
	}

	// A minMargin high enough to exclude every Opportunity should show
	// the empty state instead of a stale/wrong table.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/?hub=Jita&minMargin=1000000", nil)
	srv.ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "No Opportunities match the current filters.") {
		t.Errorf("body should show the empty state for an unreachable minMargin:\n%s", rec2.Body.String())
	}
}

// TestDashboardFiltersByMinMarkup is the AC-mandated end-to-end test:
// the Markup filter applies independently of the ISK-based Margin
// filter, and its value round-trips through the rendered input in the
// same percentage units it was submitted in.
func TestDashboardFiltersByMinMarkup(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.TokenResp.AccessToken = fakeCharacterAccessToken(t, 95465499)
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 100, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 5.40},
		{OrderID: 101, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: true, Price: 4.50},
	}
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 500000}}
	srv := newTestServer(t, fake)
	authenticate(t, srv)

	// This Opportunity's Markup is a few percent (Margin/BestBuy on a
	// 4.50/5.40 spread at base fee rates) -- a minMargin low enough to
	// pass alone, paired with a minMarkup far above what's achievable,
	// must still exclude it: both floors apply independently.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?hub=Jita&minMargin=0.01&minMarkup=50", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No Opportunities match the current filters.") {
		t.Errorf("an unreachable minMarkup should exclude the Opportunity even though minMargin passes:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `name="minMarkup" value="50"`) {
		t.Errorf("minMarkup input should round-trip the submitted percentage (50), not the internal fraction:\n%s", rec.Body.String())
	}

	// A minMarkup within reach must let it back through.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/?hub=Jita&minMargin=0.01&minMarkup=1", nil)
	srv.ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "Tritanium") {
		t.Errorf("a reachable minMarkup should still show the Opportunity:\n%s", rec2.Body.String())
	}
}
