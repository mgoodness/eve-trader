package esi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func TestSkillsReturnsActiveSkillLevelsByID(t *testing.T) {
	var gotAuth, gotCompatDate string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/characters/932683762/skills/" {
			t.Errorf("got path %q, want /characters/932683762/skills/", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotCompatDate = r.Header.Get("X-Compatibility-Date")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_sp": 12345,
			"skills": []map[string]any{
				{"skill_id": 3443, "active_skill_level": 4, "trained_skill_level": 4, "skillpoints_in_skill": 1},
				{"skill_id": 3446, "active_skill_level": 4, "trained_skill_level": 4, "skillpoints_in_skill": 1},
				{"skill_id": 16622, "active_skill_level": 3, "trained_skill_level": 3, "skillpoints_in_skill": 1},
			},
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL, CompatDate: "2026-09-30"})

	skills, err := client.Skills(t.Context(), 932683762, "access-token")
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}

	want := map[int32]int{3443: 4, 3446: 4, 16622: 3}
	if len(skills) != len(want) {
		t.Fatalf("got %d skills, want %d: %+v", len(skills), len(want), skills)
	}
	for id, level := range want {
		if skills[id] != level {
			t.Errorf("got skill %d level %d, want %d", id, skills[id], level)
		}
	}
	if gotAuth != "Bearer access-token" {
		t.Errorf("got Authorization %q, want %q", gotAuth, "Bearer access-token")
	}
	if gotCompatDate != "2026-09-30" {
		t.Errorf("got X-Compatibility-Date %q, want %q", gotCompatDate, "2026-09-30")
	}
}

func TestStandingsReturnsEveryEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/characters/932683762/standings/" {
			t.Errorf("got path %q, want /characters/932683762/standings/", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"from_id": 1000049, "from_type": "npc_corp", "standing": 2.5},
			{"from_id": 500002, "from_type": "faction", "standing": 1.0},
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{BaseURL: server.URL})

	standings, err := client.Standings(t.Context(), 932683762, "access-token")
	if err != nil {
		t.Fatalf("Standings: %v", err)
	}

	if len(standings) != 2 {
		t.Fatalf("got %d standings, want 2: %+v", len(standings), standings)
	}
	if standings[0].FromID != 1000049 || standings[0].FromType != "npc_corp" || standings[0].Standing != 2.5 {
		t.Errorf("got standings[0]=%+v, want {FromID:1000049 FromType:npc_corp Standing:2.5}", standings[0])
	}
	if standings[1].FromID != 500002 || standings[1].FromType != "faction" || standings[1].Standing != 1.0 {
		t.Errorf("got standings[1]=%+v, want {FromID:500002 FromType:faction Standing:1}", standings[1])
	}
}
