package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/scanner"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// TestBackgroundCycleScansEveryHubAndFiresAlerts is the AC-mandated
// test: a background cycle, run independent of any HTTP request,
// evaluates the authenticated character's Orders (firing a Discord
// Alert exactly as a dashboard request would) and scans every Hub, not
// just whichever one might be "selected".
func TestBackgroundCycleScansEveryHubAndFiresAlerts(t *testing.T) {
	var postCount int32
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postCount, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	store := openDashboardStore(t)
	if err := store.SaveToken(context.Background(), fakeToken(t)); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	fake := esi.NewFakeClient()
	// fake.Orders[0]: TypeID 34 (Tritanium), LocationID 60003760 (Jita),
	// sell @ 5.5 -- undercut by the competing 5.40 below.
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 999, TypeID: 34, LocationID: 60003760, IsBuyOrder: false, Price: 5.40},
	}
	notifier := notify.New(discord.URL, "https://eve-trader.example/")

	runBackgroundCycle(context.Background(), fake, store, notifier, time.Now())

	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Errorf("Discord POSTs = %d, want 1 (Alert must fire from a background cycle, not only a request)", got)
	}

	for _, h := range hub.All {
		cache, err := store.LoadOpportunityCache(context.Background(), h.Name)
		if err != nil {
			t.Fatalf("LoadOpportunityCache(%s): %v", h.Name, err)
		}
		if len(cache) == 0 {
			t.Errorf("opportunity cache for %s is empty, want every Hub scanned by a background cycle, not just one", h.Name)
		}
	}
}

// TestBackgroundCycleSkipsGracefullyWithNoAuthenticatedCharacter proves
// a cycle with no stored OAuth token is a no-op, not an error or a
// crash -- there's nobody to evaluate Orders for yet.
func TestBackgroundCycleSkipsGracefullyWithNoAuthenticatedCharacter(t *testing.T) {
	var postCount int32
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postCount, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	store := openDashboardStore(t)
	fake := esi.NewFakeClient()
	notifier := notify.New(discord.URL, "https://eve-trader.example/")

	runBackgroundCycle(context.Background(), fake, store, notifier, time.Now())

	if got := atomic.LoadInt32(&postCount); got != 0 {
		t.Errorf("Discord POSTs = %d, want 0 (nobody authenticated, nothing to evaluate)", got)
	}
	if calls := fake.MarketOrdersCalls.Load(); calls != 0 {
		t.Errorf("MarketOrdersCalls = %d, want 0 (must not scan when nobody's authenticated)", calls)
	}
}

// TestBackgroundCycleDoesNotDoubleFireAgainstARequestDrivenCycle is the
// AC-mandated test for the shared-throttle guarantee: an Alert that
// already fired during a request-driven cycle (buildDashboardView) must
// not fire again from a background cycle touching the same Order
// shortly after, since both share the same underlying throttle state.
func TestBackgroundCycleDoesNotDoubleFireAgainstARequestDrivenCycle(t *testing.T) {
	var postCount int32
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postCount, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	store := openDashboardStore(t)
	if err := store.SaveToken(context.Background(), fakeToken(t)); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 999, TypeID: 34, LocationID: 60003760, IsBuyOrder: false, Price: 5.40},
	}
	notifier := notify.New(discord.URL, "https://eve-trader.example/")

	now := time.Now()
	if _, err := buildDashboardView(context.Background(), fake, store, notifier, 95465499, hub.Jita, scanner.Filter{}, now); err != nil {
		t.Fatalf("buildDashboardView: %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Fatalf("Discord POSTs after request-driven cycle = %d, want 1", got)
	}

	// Well within the 4h throttle window.
	runBackgroundCycle(context.Background(), fake, store, notifier, now.Add(10*time.Minute))

	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Errorf("Discord POSTs after background cycle = %d, want still 1 (must not double-fire an already-throttled Alert)", got)
	}
}

// fakeToken builds a storage.Token whose access token JWT-encodes
// fakeCharacterAccessToken's canned character (95465499), so
// auth.CurrentCharacterID resolves successfully.
func fakeToken(t *testing.T) storage.Token {
	t.Helper()
	return storage.Token{
		AccessToken:  fakeCharacterAccessToken(t, 95465499),
		RefreshToken: "fake-refresh-token",
		ExpiresAt:    time.Now().Add(20 * time.Minute),
	}
}
