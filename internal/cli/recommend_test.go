package cli_test

import (
	"encoding/base64"
	"encoding/json"
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

func testConfig(t *testing.T, esiBaseURL string) cli.Config {
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = esiBaseURL
	cfg.SSOBaseURL = esiBaseURL
	cfg.CacheDir = t.TempDir()
	// A temp state dir keeps the trading-stock ledger BuildResult now reads
	// and writes out of the real user state directory.
	cfg.StateDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "client-id",
		RefreshToken: "original-refresh-token",
	}
	return cfg
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

// TestRecommendJSONCommandEmitsEmptyArraysNotNullForAnEmptyUniverse pins the
// machine contract's []-not-null guarantee on the command's own output (spec
// §14): an adapter consuming --json from a zero-candidate run still gets an
// array for every list, ready to index.
func TestRecommendJSONCommandEmitsEmptyArraysNotNullForAnEmptyUniverse(t *testing.T) {
	server, _ := historyFilteredFixtureServer(t, nil, nil)
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
	// Unmarshalling null yields a nil slice, [] an empty non-nil one, so
	// this distinguishes the contract's arrays from a null placeholder.
	if result.BuyRecommendations == nil || result.Unfunded == nil || result.Excluded == nil ||
		result.SellRecommendations == nil || result.Pending == nil {
		t.Errorf("got a nil list in %+v, want [] not null:\n%s", result, out.String())
	}
	if strings.Contains(out.String(), ": null") {
		t.Errorf("got JSON containing a null list, want []:\n%s", out.String())
	}
}

func TestRecommendJSONCommandEmitsTheJSONResult(t *testing.T) {
	// ADV 1000, not the usual fixture's 100: a 100-ADV cap floors to zero
	// under UnitRoundingStep, which would leave Morphite unfunded here.
	history := map[int32][]map[string]any{11399: historyDays(30, 1000, 30000, 15000)}
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
	if len(result.BuyRecommendations) != 1 {
		t.Errorf("got %d recommendations, want 1", len(result.BuyRecommendations))
	}
	if result.Summary.Excluded != 1 {
		t.Errorf("got Excluded=%d, want 1 (Tritanium)", result.Summary.Excluded)
	}
	if result.BuyRecommendations[0].RoiPerDay == 0 {
		t.Errorf("got RoiPerDay=0, want it populated in the JSON contract")
	}
	if result.BuyRecommendations[0].Name != "Morphite" {
		t.Errorf("got Name=%q, want the resolved Morphite from /universe/names/", result.BuyRecommendations[0].Name)
	}
	if result.BuyRecommendations[0].Flags == nil {
		t.Errorf("got nil Flags in the JSON contract, want [] not null")
	}
}

func TestRecommendDeltaFlagOverridesTheConfiguredValue(t *testing.T) {
	// ADV 1000, not the usual fixture's 100: a 100-ADV cap floors to zero
	// under UnitRoundingStep, which would leave Morphite unfunded here.
	history := map[int32][]map[string]any{11399: historyDays(30, 1000, 30000, 15000)}
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
	if len(result.BuyRecommendations) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(result.BuyRecommendations), result.BuyRecommendations)
	}
	rec := result.BuyRecommendations[0]
	// best bid 18220, best ask 24080 (see historyFilteredOrders); δ=250
	// overrides the default of 100, so buy_price/sell_price must move by
	// the difference.
	if rec.BuyPrice != 18470 || rec.SellPrice != 23830 {
		t.Errorf("got buy_price=%v sell_price=%v, want buy_price=18470 sell_price=23830 (\u03b4=250 applied)", rec.BuyPrice, rec.SellPrice)
	}
}
