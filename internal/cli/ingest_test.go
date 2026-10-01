package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/cli"
)

// pagedFeedServer serves a multi-page region-orders feed. Each page's
// ETag is derived from its content generation, so bumping generation
// changes every page's ETag; a request whose If-None-Match matches the
// page's current ETag gets a 304.
type pagedFeedServer struct {
	mu         sync.Mutex
	pages      int
	generation int
	requests   []string
}

func newPagedFeedServer(pages int) *pagedFeedServer {
	return &pagedFeedServer{pages: pages, generation: 1}
}

func (s *pagedFeedServer) etag(page int) string {
	return fmt.Sprintf(`"gen%d-page%d"`, s.generation, page)
}

func (s *pagedFeedServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.requests = append(s.requests, r.URL.RequestURI())

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		etag := s.etag(page)

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("X-Pages", strconv.Itoa(s.pages))
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		price := float64(page) + float64(s.generation)*100
		order := map[string]any{
			"order_id": page, "type_id": 30 + page, "location_id": 60004588, "system_id": 30002510,
			"volume_total": 1, "volume_remain": 1, "min_volume": 1, "price": price, "is_buy_order": false, "range": "region",
		}
		json.NewEncoder(w).Encode([]map[string]any{order})
	}
}

func (s *pagedFeedServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *pagedFeedServer) bumpGeneration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
}

func TestRegionFeedFetchesEveryPageAndDecodesAllOrders(t *testing.T) {
	feed := newPagedFeedServer(3)
	server := httptest.NewServer(feed.handler())
	defer server.Close()

	cfg := testConfig(t, server.URL)

	orders, err := cli.RegionFeed(t.Context(), cfg)
	if err != nil {
		t.Fatalf("RegionFeed: %v", err)
	}

	if len(orders) != 3 {
		t.Fatalf("got %d orders, want 3 (one per page): %+v", len(orders), orders)
	}
	if feed.requestCount() != 3 {
		t.Fatalf("got %d requests, want 3 (one per page)", feed.requestCount())
	}
}

func TestRegionFeedServesFromCacheWithoutARequestWhileFresh(t *testing.T) {
	feed := newPagedFeedServer(2)
	server := httptest.NewServer(feed.handler())
	defer server.Close()

	cfg := testConfig(t, server.URL)

	if _, err := cli.RegionFeed(t.Context(), cfg); err != nil {
		t.Fatalf("first RegionFeed: %v", err)
	}
	firstCount := feed.requestCount()

	orders, err := cli.RegionFeed(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second RegionFeed: %v", err)
	}

	if feed.requestCount() != firstCount {
		t.Errorf("got %d requests after a second call within the TTL, want %d (served from cache)", feed.requestCount(), firstCount)
	}
	if len(orders) != 2 {
		t.Errorf("got %d orders from the cached second call, want 2", len(orders))
	}
}

func TestRegionFeedHonoursA304OnAStaleCacheEntryAndRefreshesItsTTL(t *testing.T) {
	feed := newPagedFeedServer(2)
	server := httptest.NewServer(feed.handler())
	defer server.Close()

	cfg := testConfig(t, server.URL)

	if _, err := cli.RegionFeed(t.Context(), cfg); err != nil {
		t.Fatalf("first RegionFeed: %v", err)
	}

	// Force the cached pages to go stale without changing server content,
	// so a second call must conditionally revalidate (and get 304s) rather
	// than treat the cache as missing.
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	for _, key := range []string{"region-orders:10000030:page:1", "region-orders:10000030:page:2"} {
		body, etag, _, err := store.GetWithETag(key)
		if err != nil {
			t.Fatalf("GetWithETag(%q): %v", key, err)
		}
		if body == nil {
			t.Fatalf("expected a cached entry for %q before expiring it", key)
		}
		if err := store.SetWithETag(key, body, etag, -time.Second); err != nil {
			t.Fatalf("SetWithETag(%q): %v", key, err)
		}
	}

	orders, err := cli.RegionFeed(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second RegionFeed: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("got %d orders after a 304 revalidation, want 2 (reused from cache): %+v", len(orders), orders)
	}
	if feed.requestCount() != 4 {
		t.Fatalf("got %d requests, want 4 (2 initial + 2 conditional revalidations)", feed.requestCount())
	}

	// The revalidated entries must be fresh again.
	for _, key := range []string{"region-orders:10000030:page:1", "region-orders:10000030:page:2"} {
		_, _, fresh, err := store.GetWithETag(key)
		if err != nil {
			t.Fatalf("GetWithETag(%q): %v", key, err)
		}
		if !fresh {
			t.Errorf("got fresh=false for %q after a 304 revalidation, want true (TTL refreshed)", key)
		}
	}
}

func TestRegionFeedRefetchesChangedPagesOnceStale(t *testing.T) {
	feed := newPagedFeedServer(1)
	server := httptest.NewServer(feed.handler())
	defer server.Close()

	cfg := testConfig(t, server.URL)

	orders, err := cli.RegionFeed(t.Context(), cfg)
	if err != nil {
		t.Fatalf("first RegionFeed: %v", err)
	}
	if orders[0].Price != 101 {
		t.Fatalf("got price %v, want 101 (generation 1, page 1)", orders[0].Price)
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	key := "region-orders:10000030:page:1"
	body, etag, _, err := store.GetWithETag(key)
	if err != nil {
		t.Fatalf("GetWithETag: %v", err)
	}
	if err := store.SetWithETag(key, body, etag, -time.Second); err != nil {
		t.Fatalf("SetWithETag: %v", err)
	}
	feed.bumpGeneration() // server content changes; If-None-Match no longer matches

	orders, err = cli.RegionFeed(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second RegionFeed: %v", err)
	}
	if len(orders) != 1 || orders[0].Price != 201 {
		t.Fatalf("got orders %+v, want one order at price 201 (generation 2, page 1: a changed page must be refetched, not served stale from cache)", orders)
	}
}
