package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

func fakeESIServer(t *testing.T) *httptest.Server {
	t.Helper()
	orders := []map[string]any{
		{"order_id": 1, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 10, "volume_remain": 10, "min_volume": 1, "price": 24080.0, "is_buy_order": false, "range": "region"},
		{"order_id": 2, "type_id": 11399, "location_id": 60004588, "system_id": 30002510, "volume_total": 5, "volume_remain": 5, "min_volume": 1, "price": 18220.0, "is_buy_order": true, "range": "station"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Pages", "1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}))
	t.Cleanup(server.Close)
	return server
}

func testConfig(t *testing.T, esiBaseURL string) cli.Config {
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = esiBaseURL
	cfg.CacheDir = t.TempDir()
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

func TestRecommendCommandRequiresTheJSONFlag(t *testing.T) {
	server := fakeESIServer(t)
	root := cli.NewRootCmd(testConfig(t, server.URL))
	root.SetArgs([]string{"recommend"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})

	if err := root.ExecuteContext(t.Context()); err == nil {
		t.Fatalf("got no error running recommend without --json, want an error")
	}
}

func TestRecommendJSONCommandEmitsTheJSONResult(t *testing.T) {
	server := fakeESIServer(t)
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
}

func TestRecommendDeltaFlagOverridesTheConfiguredValue(t *testing.T) {
	server := fakeESIServer(t)
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
	// best bid 18220, best ask 24080 (see fakeESIServer); δ=250 overrides the
	// default of 100, so buy_price/sell_price must move by the difference.
	if rec.BuyPrice != 18470 || rec.SellPrice != 23830 {
		t.Errorf("got buy_price=%v sell_price=%v, want buy_price=18470 sell_price=23830 (\u03b4=250 applied)", rec.BuyPrice, rec.SellPrice)
	}
}
