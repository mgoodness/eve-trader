package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestRegionOrdersFetchesAllPagesAndFiltersByType(t *testing.T) {
	page1 := []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
	}
	page2 := []map[string]any{
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
	}

	var gotPaths []string
	var gotCompatDate, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.RequestURI())
		gotCompatDate = r.Header.Get("X-Compatibility-Date")
		gotUserAgent = r.Header.Get("User-Agent")

		w.Header().Set("X-Pages", "2")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "", "1":
			json.NewEncoder(w).Encode(page1)
		case "2":
			json.NewEncoder(w).Encode(page2)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    server.URL,
		UserAgent:  "eve-trader/0.1 (test; +https://github.com/mgoodness/eve-trader)",
		CompatDate: "2026-09-30",
	})

	orders, err := client.RegionOrders(t.Context(), 10000030, "all", 11399)
	if err != nil {
		t.Fatalf("RegionOrders: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("got %d orders, want 2: %+v", len(orders), orders)
	}
	if orders[0].OrderID != 1 || orders[1].OrderID != 2 {
		t.Errorf("got order IDs %d, %d, want 1, 2", orders[0].OrderID, orders[1].OrderID)
	}
	if orders[1].IsBuyOrder != true || orders[1].Price != 18220 {
		t.Errorf("got order[1]=%+v, want a buy order at price 18220", orders[1])
	}

	if len(gotPaths) != 2 {
		t.Fatalf("got %d requests, want 2 (one per page): %v", len(gotPaths), gotPaths)
	}
	for _, p := range gotPaths {
		if want := "type_id=11399"; !strings.Contains(p, want) {
			t.Errorf("request %q missing %q", p, want)
		}
		if want := "order_type=all"; !strings.Contains(p, want) {
			t.Errorf("request %q missing %q", p, want)
		}
	}
	if gotCompatDate != "2026-09-30" {
		t.Errorf("got X-Compatibility-Date %q, want %q", gotCompatDate, "2026-09-30")
	}
	if gotUserAgent == "" {
		t.Errorf("got empty User-Agent, want a set contact string")
	}
}

func TestFetchRegionOrdersPageSendsIfNoneMatchAndReturnsTheETag(t *testing.T) {
	var gotIfNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		w.Header().Set("X-Pages", "3")
		w.Header().Set("ETag", `"v2"`)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"order_id": 1, "type_id": 34}})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL, UserAgent: "eve-trader/test"})

	result, err := client.FetchRegionOrdersPage(t.Context(), 10000030, "all", 0, 1, `"v1"`)
	if err != nil {
		t.Fatalf("FetchRegionOrdersPage: %v", err)
	}

	if gotIfNoneMatch != `"v1"` {
		t.Errorf("got If-None-Match %q, want %q", gotIfNoneMatch, `"v1"`)
	}
	if result.NotModified {
		t.Errorf("got NotModified=true for a 200 response, want false")
	}
	if result.ETag != `"v2"` {
		t.Errorf("got ETag %q, want %q", result.ETag, `"v2"`)
	}
	if result.Pages != 3 {
		t.Errorf("got Pages %d, want 3", result.Pages)
	}
	if len(result.Body) == 0 {
		t.Errorf("got empty body, want the page body")
	}
}

func TestFetchRegionOrdersPageHonoursA304(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		t.Fatalf("got unexpected request without the matching If-None-Match: %v", r.Header.Get("If-None-Match"))
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL, UserAgent: "eve-trader/test"})

	result, err := client.FetchRegionOrdersPage(t.Context(), 10000030, "all", 0, 1, `"v1"`)
	if err != nil {
		t.Fatalf("FetchRegionOrdersPage: %v", err)
	}

	if !result.NotModified {
		t.Fatalf("got NotModified=false for a 304 response, want true")
	}
	if len(result.Body) != 0 {
		t.Errorf("got a non-empty body on a 304, want empty: %s", result.Body)
	}
}

func TestFetchRegionOrdersPageBacksOffAndRetriesOn429(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("X-Pages", "1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	var slept []time.Duration
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:   server.URL,
		UserAgent: "eve-trader/test",
		Sleep:     func(d time.Duration) { slept = append(slept, d) },
	})

	result, err := client.FetchRegionOrdersPage(t.Context(), 10000030, "all", 0, 1, "")
	if err != nil {
		t.Fatalf("FetchRegionOrdersPage: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("got %d attempts, want 2 (one 429, one success)", attempts)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("got sleeps %v, want one sleep of 2s (from Retry-After)", slept)
	}
	if result.NotModified {
		t.Errorf("got NotModified=true, want false")
	}
}

func TestFetchRegionOrdersPageBacksOffAndRetriesOn420(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(420)
			return
		}
		w.Header().Set("X-Pages", "1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	var slept []time.Duration
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:   server.URL,
		UserAgent: "eve-trader/test",
		Sleep:     func(d time.Duration) { slept = append(slept, d) },
	})

	_, err := client.FetchRegionOrdersPage(t.Context(), 10000030, "all", 0, 1, "")
	if err != nil {
		t.Fatalf("FetchRegionOrdersPage: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("got %d attempts, want 2 (one 420, one success)", attempts)
	}
	if len(slept) != 1 || slept[0] <= 0 {
		t.Fatalf("got sleeps %v, want one positive backoff sleep", slept)
	}
}

func TestFetchRegionOrdersPageGivesUpAfterRepeated429s(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:   server.URL,
		UserAgent: "eve-trader/test",
		Sleep:     func(time.Duration) {},
	})

	_, err := client.FetchRegionOrdersPage(t.Context(), 10000030, "all", 0, 1, "")
	if err == nil {
		t.Fatalf("got no error after repeated 429s, want an error once retries are exhausted")
	}
	if attempts < 2 {
		t.Fatalf("got %d attempts, want at least 2 (some retrying) before giving up", attempts)
	}
}

func TestDecodeOrdersDecodesAPageBodyIntoEngineOrders(t *testing.T) {
	body := []byte(`[{"order_id":1,"type_id":34,"location_id":60004588,"system_id":30002510,"volume_total":10,"volume_remain":10,"min_volume":1,"price":5.5,"is_buy_order":false,"range":"region"}]`)

	orders, err := esi.DecodeOrders(body)
	if err != nil {
		t.Fatalf("DecodeOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1: %+v", len(orders), orders)
	}
	if orders[0].OrderID != 1 || orders[0].TypeID != 34 || orders[0].Price != 5.5 {
		t.Errorf("got order %+v, want order_id=1 type_id=34 price=5.5", orders[0])
	}
}
