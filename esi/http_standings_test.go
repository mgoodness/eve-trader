package esi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchCharacterStandings(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"from_id": 1000049, "from_type": "npc_corp", "standing": 5.5},
			{"from_id": 500002, "from_type": "faction", "standing": 8.25},
			{"from_id": 3000001, "from_type": "agent", "standing": 1.0},
		})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchCharacterStandings(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/latest/characters/123/standings/" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer access" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(got) != 3 {
		t.Fatalf("standings = %+v, want three", got)
	}
	if got[0] != (esi.Standing{FromID: 1000049, FromType: "npc_corp", Standing: 5.5}) {
		t.Errorf("corp standing = %+v", got[0])
	}
	if got[1] != (esi.Standing{FromID: 500002, FromType: "faction", Standing: 8.25}) {
		t.Errorf("faction standing = %+v", got[1])
	}
	if got[2].FromType != "agent" {
		t.Errorf("agent standing type = %q", got[2].FromType)
	}
}

func TestHTTPGatewayFetchStationOwner(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"station_id": 60004588,
			"name":       "Rens VI - Moon 8 - Brutor Tribe Treasury",
			"owner":      1000049,
		})
	}))
	defer server.Close()

	owner, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchStationOwner(context.Background(), 60004588)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/latest/universe/stations/60004588/" {
		t.Fatalf("path = %q", gotPath)
	}
	if owner != 1000049 {
		t.Fatalf("owner = %d, want 1000049", owner)
	}
}
