package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestFetchHistoryDecodesTheDailyRecords(t *testing.T) {
	var gotPath, gotCompatDate, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotCompatDate = r.Header.Get("X-Compatibility-Date")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"h1"`)
		json.NewEncoder(w).Encode([]map[string]any{
			{"date": "2024-01-01", "order_count": 5, "volume": 100, "highest": 24500.0, "average": 24200.0, "lowest": 23900.0},
			{"date": "2024-01-02", "order_count": 3, "volume": 40, "highest": 24700.0, "average": 24300.0, "lowest": 24000.0},
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    server.URL,
		UserAgent:  "eve-trader/0.1 (test; +https://github.com/mgoodness/eve-trader)",
		CompatDate: "2026-09-30",
	})

	result, err := client.FetchHistory(t.Context(), 10000030, 11399, "")
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if result.NotModified {
		t.Fatalf("got NotModified=true for a 200 response, want false")
	}
	if result.ETag != `"h1"` {
		t.Errorf("got ETag %q, want %q", result.ETag, `"h1"`)
	}
	if gotPath != "/markets/10000030/history/?type_id=11399" {
		t.Errorf("got path %q, want /markets/10000030/history/?type_id=11399", gotPath)
	}
	if gotCompatDate != "2026-09-30" {
		t.Errorf("got X-Compatibility-Date %q, want %q", gotCompatDate, "2026-09-30")
	}
	if gotUserAgent == "" {
		t.Errorf("got empty User-Agent, want a set contact string")
	}

	records, err := esi.DecodeHistory(result.Body)
	if err != nil {
		t.Fatalf("DecodeHistory: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(records), records)
	}
	if records[0].Date != "2024-01-01" || records[0].Volume != 100 || records[0].Highest != 24500.0 || records[0].Lowest != 23900.0 {
		t.Errorf("got records[0]=%+v, want date 2024-01-01, volume 100, highest 24500, lowest 23900", records[0])
	}
	if records[1].Volume != 40 {
		t.Errorf("got records[1].Volume=%v, want 40", records[1].Volume)
	}
}

func TestFetchHistorySendsIfNoneMatchAndHonoursA304(t *testing.T) {
	var gotIfNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		if gotIfNoneMatch == `"h1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		t.Fatalf("got unexpected request without the matching If-None-Match: %v", gotIfNoneMatch)
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL})

	result, err := client.FetchHistory(t.Context(), 10000030, 11399, `"h1"`)
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if !result.NotModified {
		t.Fatalf("got NotModified=false for a 304 response, want true")
	}
	if len(result.Body) != 0 {
		t.Errorf("got non-empty body on a 304, want none")
	}
}

func TestFetchHistoryReturnsAnErrorForAnUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL})

	if _, err := client.FetchHistory(t.Context(), 10000030, 11399, ""); err == nil {
		t.Fatal("FetchHistory: got no error for a 404 response, want one")
	}
}
