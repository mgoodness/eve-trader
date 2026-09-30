package cli_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

const (
	nearSystemID    int32 = 30002187 // reachable: route is 4 systems, jump distance 3
	unreachableID   int32 = 30099999 // route lookup fails (500)
	otherNPCStation int64 = 60004595 // some NPC station in nearSystemID
)

// routeFixtureServer serves GET /route/{origin}/{destination}/ for
// nearSystemID -> a 3-jump route to the trade system, and fails (500) for
// unreachableID. It counts requests per path so tests can assert caching
// behavior.
func routeFixtureServer(t *testing.T) (*httptest.Server, func(path string) int) {
	t.Helper()
	var mu sync.Mutex
	counts := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()

		switch {
		case strings.HasPrefix(r.URL.Path, "/route/30002187/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[30002510,30002200,30002199,30002187]`)) // 3 jumps
		case strings.HasPrefix(r.URL.Path, "/route/30099999/"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[path]
	}
}

func TestJumpDistancesResolvesTheDistanceForEachNumericRangeSystem(t *testing.T) {
	server, _ := routeFixtureServer(t)
	cfg := testConfig(t, server.URL)
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}

	orders := []engine.Order{
		{OrderID: 1, IsBuyOrder: true, LocationID: otherNPCStation, SystemID: nearSystemID, Price: 100, Range: "5"},
	}

	distances, warnings := cli.JumpDistances(t.Context(), cfg, store, orders)

	if distances[nearSystemID] != 3 {
		t.Errorf("got distance %d for system %d, want 3", distances[nearSystemID], nearSystemID)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}
}

func TestJumpDistancesCachesALookupAcrossCalls(t *testing.T) {
	server, requestCount := routeFixtureServer(t)
	cfg := testConfig(t, server.URL)
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}

	orders := []engine.Order{
		{OrderID: 1, IsBuyOrder: true, LocationID: otherNPCStation, SystemID: nearSystemID, Price: 100, Range: "5"},
		{OrderID: 2, IsBuyOrder: true, LocationID: otherNPCStation, SystemID: nearSystemID, Price: 200, Range: "3"}, // same system, second order: one lookup only
	}

	// First call primes the cache.
	cli.JumpDistances(t.Context(), cfg, store, orders)
	path := "/route/30002187/30002510/"
	if n := requestCount(path); n != 1 {
		t.Fatalf("got %d requests to %q after first call, want 1 (one lookup per distinct system, not per order)", n, path)
	}

	distances, _ := cli.JumpDistances(t.Context(), cfg, store, orders)
	if distances[nearSystemID] != 3 {
		t.Fatalf("got distance %d, want 3", distances[nearSystemID])
	}
	if n := requestCount(path); n != 1 {
		t.Errorf("got %d requests to %q after a second call, want 1 (cached for 86,400s, not refetched)", n, path)
	}
}

func TestJumpDistancesExcludesAFailedLookupAndRecordsAWarning(t *testing.T) {
	server, _ := routeFixtureServer(t)
	cfg := testConfig(t, server.URL)
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}

	orders := []engine.Order{
		{OrderID: 1, IsBuyOrder: true, LocationID: otherNPCStation, SystemID: unreachableID, Price: 100, Range: "5"},
	}

	distances, warnings := cli.JumpDistances(t.Context(), cfg, store, orders)

	if _, ok := distances[unreachableID]; ok {
		t.Errorf("got a distance for system %d despite a failed lookup, want none", unreachableID)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
}
