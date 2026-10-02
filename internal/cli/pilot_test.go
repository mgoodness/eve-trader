package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// realPathPilotFactsServer serves an empty region feed plus the SSO token,
// skills, and standings routes the real pipeline (cli.BuildResult) reads,
// so a test can assert on the live pilot facts a run derives with no
// market data to price.
func realPathPilotFactsServer(t *testing.T, skills, standings []map[string]any, rotatedRefreshToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/orders/history/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/orders/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/assets/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.Contains(r.URL.Path, "/orders"):
			w.Header().Set("X-Pages", "1")
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": rotatedRefreshToken,
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": skills})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode(standings)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestBuildResultDerivesFeesFromLiveSkillsAndStandingsNotAHardcodedRate
// proves the real pipeline's Meta.Fees come from the pilot facts ESI
// reports, not a compiled-in rate: different skills and non-zero standings
// must move the derived broker fee.
func TestBuildResultDerivesDifferentFeesWhenLiveSkillsAndStandingsDiffer(t *testing.T) {
	skills := []map[string]any{{"skill_id": engine.BrokerRelationsSkillID, "active_skill_level": 5}}
	standings := []map[string]any{
		{"from_id": int(cli.DefaultConfig().RegionFactionID), "from_type": "faction", "standing": 10.0},
		{"from_id": int(cli.DefaultConfig().StationOwnerCorpID), "from_type": "npc_corp", "standing": 10.0},
	}
	server := realPathPilotFactsServer(t, skills, standings, "rotated-refresh-token")

	result, _, err := cli.BuildResult(t.Context(), testConfig(t, server.URL))
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	// 3% - 0.3%*5 - 0.03%*10 - 0.02%*10 = 1.0%, the floor (spec §4).
	if result.Meta.Fees.Broker != 0.01 {
		t.Errorf("got broker fee %v, want 0.01 (floor, at Broker Relations V and standings 10/10)", result.Meta.Fees.Broker)
	}
	if result.Meta.Params.FactionStanding != 10 || result.Meta.Params.CorpStanding != 10 {
		t.Errorf("got standings %v/%v, want the live 10/10 echoed into Meta.Params",
			result.Meta.Params.FactionStanding, result.Meta.Params.CorpStanding)
	}
}

// TestBuildResultPersistsARotatedRefreshTokenToCredentialsJSON proves a
// real run, which mints its access token through pilotFacts, writes the
// refresh token ESI rotates back to credentials.json (spec §12).
func TestBuildResultPersistsARotatedRefreshTokenToCredentialsJSON(t *testing.T) {
	server := realPathPilotFactsServer(t, pilotSkills(), []map[string]any{}, "rotated-refresh-token")
	cfg := testConfig(t, server.URL)
	configDir := t.TempDir()
	cfg.ConfigDir = configDir
	credPath := filepath.Join(configDir, "credentials.json")
	if err := config.SaveCredentials(credPath, cfg.Credentials); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	if _, _, err := cli.BuildResult(t.Context(), cfg); err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	got, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want the rotated one persisted to disk", got.RefreshToken)
	}
}

// TestBuildResultFailsClearlyWithoutCredentials proves a real run without a
// stored refresh token fails with the login hint rather than a bare ESI
// error.
func TestBuildResultFailsClearlyWithoutCredentials(t *testing.T) {
	server := realPathPilotFactsServer(t, pilotSkills(), []map[string]any{}, "rotated-refresh-token")
	cfg := testConfig(t, server.URL)
	cfg.Credentials = config.Credentials{}

	_, _, err := cli.BuildResult(t.Context(), cfg)
	if err == nil {
		t.Fatal("got no error running without credentials, want a clear one")
	}
	if !strings.Contains(err.Error(), "eve-trader login") {
		t.Errorf("got error %q, want it to name `eve-trader login`", err.Error())
	}
}
