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
	}{
		{"", 0, 0},
		{"?minVolume=1000&minMargin=5.5", 1000, 5.5},
		{"?minVolume=not-a-number", 0, 0}, // invalid falls back to 0
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/"+c.query, nil)
		got := parseFilterParams(req)
		if got.MinVolume != c.wantMinVolume || got.MinMargin != c.wantMinMargin {
			t.Errorf("parseFilterParams(%q) = %+v, want {%v %v}", c.query, got, c.wantMinVolume, c.wantMinMargin)
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
