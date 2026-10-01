package cli_test

import (
	"encoding/base64"
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

// pilotSkills are the pilot's real skills from spec §4: Trade 4, Broker
// Relations 4, Accounting 3 → broker 1.8%, sales tax 5.025%, order limit
// 21 at zero standings.
func pilotSkills() []map[string]any {
	return []map[string]any{
		{"skill_id": engine.TradeSkillID, "active_skill_level": 4},
		{"skill_id": engine.BrokerRelationsSkillID, "active_skill_level": 4},
		{"skill_id": engine.AccountingSkillID, "active_skill_level": 3},
	}
}

// fakeESIServerOpts lets a test override the live pilot facts a fake ESI/SSO
// server returns, so tests can prove fees/order-limit are derived from
// whatever ESI reports rather than hardcoded.
type fakeESIServerOpts struct {
	skills           []map[string]any
	standings        []map[string]any
	rotatedRefresh   string
	characterIDClaim string
}

func defaultFakeESIServerOpts() fakeESIServerOpts {
	return fakeESIServerOpts{
		skills:           pilotSkills(),
		standings:        []map[string]any{},
		rotatedRefresh:   "rotated-refresh-token",
		characterIDClaim: "CHARACTER:EVE:932683762",
	}
}

// fakeJWT builds an unsigned JWT-shaped access token whose payload carries
// sub. Only the payload matters to the code under test.
func fakeJWT(sub string) string {
	return fakeJWTWithName(sub, "")
}

// fakeJWTWithName is fakeJWT plus a name claim, for tests that assert on
// `login`'s printed character name (docs/research/esi-sso-cli.md §4.3:
// the JWT carries name alongside sub). An empty name omits the claim
// entirely, matching a real token that (per the research note) always has
// one, but keeping fakeJWT's existing callers -- which don't care about
// name -- unchanged.
func fakeJWTWithName(sub, name string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := map[string]string{"sub": sub}
	if name != "" {
		claims["name"] = name
	}
	payload, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func fakeESIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return fakeESIServerWithOpts(t, defaultFakeESIServerOpts())
}

func fakeESIServerWithOpts(t *testing.T, opts fakeESIServerOpts) *httptest.Server {
	t.Helper()
	orders := []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.Contains(r.URL.Path, "/orders"):
			w.Header().Set("X-Pages", "1")
			json.NewEncoder(w).Encode(orders)
		case r.URL.Path == "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT(opts.characterIDClaim),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": opts.rotatedRefresh,
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": opts.skills})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode(opts.standings)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func testConfig(t *testing.T, esiBaseURL string) cli.Config {
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = esiBaseURL
	cfg.SSOBaseURL = esiBaseURL
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "client-id",
		RefreshToken: "original-refresh-token",
	}
	return cfg
}

func TestRunFetchesOrdersPricesAndEmitsTheJSONResult(t *testing.T) {
	server := fakeESIServer(t)
	cfg := testConfig(t, server.URL)

	var buf strings.Builder
	if err := cli.Run(t.Context(), cfg, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var result engine.Result
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if len(result.Recommendations) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(result.Recommendations), result.Recommendations)
	}
	rec := result.Recommendations[0]
	if rec.TypeID != cfg.TypeID || rec.BuyPrice != 18320 || rec.SellPrice != 23980 {
		t.Errorf("got recommendation %+v, want type_id=%d buy_price=18320 sell_price=23980", rec, cfg.TypeID)
	}
}

func TestRunDerivesFeesFromLiveSkillsAndStandingsNotAHardcodedRate(t *testing.T) {
	server := fakeESIServer(t)
	cfg := testConfig(t, server.URL)

	var buf strings.Builder
	if err := cli.Run(t.Context(), cfg, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var result engine.Result
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	// Pilot's real skills (Trade 4, Broker Relations 4, Accounting 3) at
	// zero standings (spec §4): broker 1.8%, sales tax 5.025%.
	if result.Meta.Fees.Broker != 0.018 {
		t.Errorf("got broker fee %v, want 0.018 (derived from live skills)", result.Meta.Fees.Broker)
	}
	if result.Meta.Fees.SalesTax != 0.05025 {
		t.Errorf("got sales tax %v, want 0.05025 (derived from live skills)", result.Meta.Fees.SalesTax)
	}
}

func TestRunDerivesDifferentFeesWhenLiveSkillsAndStandingsDiffer(t *testing.T) {
	opts := defaultFakeESIServerOpts()
	opts.skills = []map[string]any{{"skill_id": engine.BrokerRelationsSkillID, "active_skill_level": 5}}
	opts.standings = []map[string]any{
		{"from_id": int(cli.DefaultConfig().RegionFactionID), "from_type": "faction", "standing": 10.0},
		{"from_id": int(cli.DefaultConfig().StationOwnerCorpID), "from_type": "npc_corp", "standing": 10.0},
	}
	server := fakeESIServerWithOpts(t, opts)
	cfg := testConfig(t, server.URL)

	var buf strings.Builder
	if err := cli.Run(t.Context(), cfg, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var result engine.Result
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	// 3% - 0.3%*5 - 0.03%*10 - 0.02%*10 = 1.0%, the floor (spec §4).
	if result.Meta.Fees.Broker != 0.01 {
		t.Errorf("got broker fee %v, want 0.01 (floor, at Broker Relations V and standings 10/10)", result.Meta.Fees.Broker)
	}
}

func TestRunPersistsARotatedRefreshTokenToCredentialsJSON(t *testing.T) {
	server := fakeESIServer(t)
	cfg := testConfig(t, server.URL)
	configDir := t.TempDir()
	cfg.ConfigDir = configDir
	credPath := filepath.Join(configDir, "credentials.json")
	if err := config.SaveCredentials(credPath, cfg.Credentials); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	var buf strings.Builder
	if err := cli.Run(t.Context(), cfg, &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want the rotated one persisted to disk", got.RefreshToken)
	}
}

func TestRunFailsClearlyWithoutCredentials(t *testing.T) {
	server := fakeESIServer(t)
	cfg := testConfig(t, server.URL)
	cfg.Credentials = config.Credentials{}

	var buf strings.Builder
	err := cli.Run(t.Context(), cfg, &buf)
	if err == nil {
		t.Fatal("got no error running without credentials, want a clear one")
	}
	if !strings.Contains(err.Error(), "eve-trader login") {
		t.Errorf("got error %q, want it to name `eve-trader login`", err.Error())
	}
}

func TestRecommendCommandDefaultsToTheDenseTableWithoutJSON(t *testing.T) {
	history := map[int32][]map[string]any{11399: historyDays(30, 100, 30000, 15000)}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	root := cli.NewRootCmd(testConfig(t, server.URL))
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "FUNDED RECOMMENDATIONS") {
		t.Fatalf("got table %q, want the funded recommendations section by default", out.String())
	}
	// Morphite's real name is resolved through the batched type-name lookup,
	// so the table shows it rather than a type-id placeholder.
	if !strings.Contains(out.String(), "Morphite") {
		t.Errorf("got table %q, want the funded Morphite recommendation", out.String())
	}
	if strings.Contains(out.String(), "thin book") {
		t.Errorf("got table %q, want excluded reasons hidden without --explain", out.String())
	}
}

func TestRecommendExplainFlagListsExcludedReasons(t *testing.T) {
	history := map[int32][]map[string]any{11399: historyDays(30, 100, 30000, 15000)}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	root := cli.NewRootCmd(testConfig(t, server.URL))
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend", "--explain"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Tritanium's real name is resolved through the batched type-name lookup,
	// so --explain names it alongside its reason.
	if !strings.Contains(out.String(), "Tritanium") || !strings.Contains(out.String(), "thin book") {
		t.Errorf("got table %q, want Tritanium and its exclusion reason under --explain", out.String())
	}
}

func TestRecommendCommandRendersAnEmptyUniverseCleanlyWithoutCrashing(t *testing.T) {
	server, _ := historyFilteredFixtureServer(t, nil, nil)
	root := cli.NewRootCmd(testConfig(t, server.URL))
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend", "--explain"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "FUNDED RECOMMENDATIONS (0)") {
		t.Errorf("got table %q, want a clean zero-candidate rendering", out.String())
	}
}

func TestRecommendJSONCommandEmitsTheJSONResult(t *testing.T) {
	history := map[int32][]map[string]any{11399: historyDays(30, 100, 30000, 15000)}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	root := cli.NewRootCmd(testConfig(t, server.URL))
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend", "--json"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result engine.Result
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if len(result.Recommendations) != 1 {
		t.Errorf("got %d recommendations, want 1", len(result.Recommendations))
	}
	if result.Summary.Excluded != 1 {
		t.Errorf("got Excluded=%d, want 1 (Tritanium)", result.Summary.Excluded)
	}
	if result.Recommendations[0].RoiPerDay == 0 {
		t.Errorf("got RoiPerDay=0, want it populated in the JSON contract")
	}
	if result.Recommendations[0].Name != "Morphite" {
		t.Errorf("got Name=%q, want the resolved Morphite from /universe/names/", result.Recommendations[0].Name)
	}
	if result.Recommendations[0].Flags == nil {
		t.Errorf("got nil Flags in the JSON contract, want [] not null")
	}
}

func TestRecommendDeltaFlagOverridesTheConfiguredValue(t *testing.T) {
	history := map[int32][]map[string]any{11399: historyDays(30, 100, 30000, 15000)}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend", "--json", "--delta", "250"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result engine.Result
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if len(result.Recommendations) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(result.Recommendations), result.Recommendations)
	}
	rec := result.Recommendations[0]
	// best bid 18220, best ask 24080 (see historyFilteredOrders); δ=250
	// overrides the default of 100, so buy_price/sell_price must move by
	// the difference.
	if rec.BuyPrice != 18470 || rec.SellPrice != 23830 {
		t.Errorf("got buy_price=%v sell_price=%v, want buy_price=18470 sell_price=23830 (\u03b4=250 applied)", rec.BuyPrice, rec.SellPrice)
	}
}
