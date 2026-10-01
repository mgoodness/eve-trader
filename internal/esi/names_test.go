package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestNamesResolvesTypeIDsViaPostUniverseNames(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotIDs []int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotIDs); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 34, "name": "Tritanium", "category": "inventory_type"},
			{"id": 11399, "name": "Morphite", "category": "inventory_type"},
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL, UserAgent: "eve-trader/test"})

	names, err := client.Names(t.Context(), []int32{34, 11399})
	if err != nil {
		t.Fatalf("Names: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %s, want POST", gotMethod)
	}
	if gotPath != "/universe/names/" {
		t.Errorf("got path %s, want /universe/names/", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("got Content-Type %q, want application/json", gotContentType)
	}
	if len(gotIDs) != 2 || gotIDs[0] != 34 || gotIDs[1] != 11399 {
		t.Errorf("got request ids %v, want [34 11399]", gotIDs)
	}
	if names[34] != "Tritanium" || names[11399] != "Morphite" {
		t.Errorf("got names %v, want 34=Tritanium 11399=Morphite", names)
	}
}

func TestNamesReturnsAClearErrorOnANon200Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL, UserAgent: "eve-trader/test"})

	_, err := client.Names(t.Context(), []int32{34})
	if err == nil {
		t.Fatal("got no error for a non-200 response, want one")
	}
}
