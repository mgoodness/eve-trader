package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestRouteReturnsTheSystemsAlongTheShortestPath(t *testing.T) {
	var gotPath, gotCompatDate, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCompatDate = r.Header.Get("X-Compatibility-Date")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]int32{30002510, 30002187, 30002053})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    server.URL,
		UserAgent:  "eve-trader/0.1 (test; +https://github.com/mgoodness/eve-trader)",
		CompatDate: "2026-09-30",
	})

	route, err := client.Route(t.Context(), 30002053, 30002510)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	want := []int32{30002510, 30002187, 30002053}
	if len(route) != len(want) {
		t.Fatalf("got route %v, want %v", route, want)
	}
	for i, id := range want {
		if route[i] != id {
			t.Errorf("got route %v, want %v", route, want)
			break
		}
	}
	if gotPath != "/route/30002053/30002510/" {
		t.Errorf("got path %q, want /route/30002053/30002510/", gotPath)
	}
	if gotCompatDate != "2026-09-30" {
		t.Errorf("got X-Compatibility-Date %q, want %q", gotCompatDate, "2026-09-30")
	}
	if gotUserAgent == "" {
		t.Errorf("got empty User-Agent, want a set contact string")
	}
}

func TestRouteReturnsAnErrorForAnUnreachableOrUnknownSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL})

	if _, err := client.Route(t.Context(), 30002053, 30002510); err == nil {
		t.Fatal("Route: got no error for a 404 response, want one")
	}
}
