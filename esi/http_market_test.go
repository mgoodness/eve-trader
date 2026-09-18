package esi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchRensOrdersPaginatesWithoutNameLookups(t *testing.T) {
	var orderPages = map[string]any{
		"1": []map[string]any{{"order_id": 1, "type_id": 34, "location_id": 60004588, "issued": "2026-09-16T00:00:00Z"}},
		"2": []map[string]any{{"order_id": 2, "type_id": 34, "location_id": 60004588, "issued": "2026-09-16T00:00:00Z"}},
	}
	var nameRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest/markets/10000030/orders/":
			page := r.URL.Query().Get("page")
			w.Header().Set("X-Pages", "2")
			if err := json.NewEncoder(w).Encode(orderPages[page]); err != nil {
				t.Errorf("encoding orders: %v", err)
			}
		case "/latest/universe/names/", "/latest/universe/types/34/":
			nameRequests++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchRensOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TypeID != 34 || got[1].TypeID != 34 {
		t.Fatalf("orders = %+v", got)
	}
	if nameRequests != 0 {
		t.Fatalf("name-lookup requests during order fetch = %d, want 0", nameRequests)
	}
}

func TestHTTPGatewayFetchTypeNamesBatchesRequests(t *testing.T) {
	const wantBatchLimit = 1000
	const idCount = 2500

	want := make(map[int]string, idCount)
	for id := 1; id <= idCount; id++ {
		want[id] = fmt.Sprintf("Name %d", id)
	}

	var batches []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/latest/universe/names/" {
			t.Errorf("path = %q, want /latest/universe/names/", r.URL.Path)
		}
		var ids []int
		if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
			t.Errorf("decoding names request: %v", err)
			return
		}
		batches = append(batches, len(ids))
		resp := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			resp = append(resp, map[string]any{"id": id, "name": want[id], "category": "inventory_type"})
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encoding names response: %v", err)
		}
	}))
	defer server.Close()

	ids := make([]int, 0, idCount)
	for id := 1; id <= idCount; id++ {
		ids = append(ids, id)
	}
	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchTypeNames(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}

	if len(batches) != 3 {
		t.Fatalf("name requests = %d (%v), want 3", len(batches), batches)
	}
	for _, size := range batches {
		if size > wantBatchLimit {
			t.Fatalf("batch size %d exceeds ESI limit %d", size, wantBatchLimit)
		}
	}
	if len(got) != idCount {
		t.Fatalf("resolved names = %d, want %d", len(got), idCount)
	}
	for id, name := range want {
		if got[id] != name {
			t.Fatalf("got[%d] = %q, want %q", id, got[id], name)
		}
	}
}

func TestHTTPGatewayFetchHistorySurfacesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.Header().Set("X-Esi-Error-Limit-Remain", "0")
		w.Header().Set("X-Esi-Error-Limit-Reset", "7")
		w.WriteHeader(420)
	}))
	defer server.Close()

	_, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchHistory(context.Background(), 34)
	var rateLimited *esi.RateLimited
	if !errors.As(err, &rateLimited) {
		t.Fatalf("FetchHistory() error = %v, want *esi.RateLimited", err)
	}
	if rateLimited.RetryAfter != 42*time.Second {
		t.Fatalf("RetryAfter = %v, want 42s", rateLimited.RetryAfter)
	}
	if rateLimited.ErrorLimitRemain != 0 || rateLimited.ErrorLimitReset != 7*time.Second {
		t.Fatalf("error-limit fields = remain %d reset %v, want 0 and 7s", rateLimited.ErrorLimitRemain, rateLimited.ErrorLimitReset)
	}
}

func TestHTTPGatewayFetchHistorySurfacesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchHistory(context.Background(), 34)
	var httpErr *esi.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("FetchHistory() error = %v, want *esi.HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", httpErr.StatusCode)
	}
}

func TestHTTPGatewayFetchHistoryCarriesPriceFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"date":"2026-09-16","volume":100,"order_count":7,"average":12.5,"highest":15.0,"lowest":10.25}]`)
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchHistory(context.Background(), 34)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("FetchHistory() = %+v, want one point", got)
	}
	point := got[0]
	want := esi.HistoryPoint{Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC), Volume: 100, OrderCount: 7, Average: 12.5, Highest: 15.0, Lowest: 10.25}
	if point != want {
		t.Fatalf("FetchHistory()[0] = %+v, want %+v", point, want)
	}
}

func TestHTTPGatewayFetchHistoryRejectsMalformedDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"date":"not-a-date","volume":1,"order_count":1}]`)
	}))
	defer server.Close()

	if _, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchHistory(context.Background(), 34); err == nil {
		t.Fatal("FetchHistory() error = nil, want malformed-date error")
	}
}
