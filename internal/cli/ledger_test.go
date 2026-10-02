package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// ledgerFixture is the state the fake character routes serve.
type ledgerFixture struct {
	orders  []map[string]any
	history []map[string]any
	assets  []map[string]any
}

// ledgerFixtureServer serves the SSO/character routes ReconcileLedger needs
// and counts hits per route, so a test can prove the ESI fetches are cached
// across runs.
func ledgerFixtureServer(t *testing.T, fx ledgerFixture) (*httptest.Server, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	hits := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/orders/history/"):
			mu.Lock()
			hits["history"]++
			mu.Unlock()
			json.NewEncoder(w).Encode(fx.history)
		case strings.HasSuffix(r.URL.Path, "/orders/"):
			mu.Lock()
			hits["orders"]++
			mu.Unlock()
			json.NewEncoder(w).Encode(fx.orders)
		case strings.HasSuffix(r.URL.Path, "/assets/"):
			mu.Lock()
			hits["assets"]++
			mu.Unlock()
			json.NewEncoder(w).Encode(fx.assets)
		case r.URL.Path == "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "rotated-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": pilotSkills()})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/universe/names/":
			var ids []int32
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Errorf("decoding /universe/names/ request: %v", err)
			}
			resolved := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				if name, ok := fixtureTypeNames[id]; ok {
					resolved = append(resolved, map[string]any{"id": id, "name": name, "category": "inventory_type"})
				}
			}
			json.NewEncoder(w).Encode(resolved)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server, func(kind string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[kind]
	}
}

func ledgerTestConfig(t *testing.T, serverURL string) cli.Config {
	t.Helper()
	cfg := testConfig(t, serverURL)
	cfg.StateDir = t.TempDir()
	return cfg
}

func writeLedgerFile(t *testing.T, cfg cli.Config, lots []engine.Lot) string {
	t.Helper()
	path := filepath.Join(cfg.StateDir, "ledger.json")
	if err := cli.SaveLedger(path, lots); err != nil {
		t.Fatalf("SaveLedger: %v", err)
	}
	return path
}

func TestLoadLedgerMissingFileIsNotAnError(t *testing.T) {
	lots, err := cli.LoadLedger(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if lots != nil {
		t.Errorf("got lots %+v, want nil for a missing ledger", lots)
	}
}

func TestSaveLedgerRoundTripsAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "ledger.json")
	want := []engine.Lot{{
		LotID:             "lot-1",
		TypeID:            34,
		SourceOrderID:     "seeded",
		QuantityTotal:     100,
		QuantityAvailable: 100,
		AcquiredAt:        time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Status:            engine.LotHeldUnlisted,
	}}

	if err := cli.SaveLedger(path, want); err != nil {
		t.Fatalf("SaveLedger: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat ledger: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("ledger mode = %#o, want 0600", perm)
	}

	// Atomic write leaves no temp files behind in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file %q left behind after SaveLedger", e.Name())
		}
	}

	// Simulate a restart: a fresh read sees exactly what was written.
	got, err := cli.LoadLedger(path)
	if err != nil {
		t.Fatalf("LoadLedger after restart: %v", err)
	}
	if len(got) != 1 || got[0].LotID != "lot-1" || got[0].QuantityAvailable != 100 || got[0].Status != engine.LotHeldUnlisted {
		t.Errorf("got %+v, want the saved lot", got)
	}
}

func TestLedgerPathResolvesUnderTheStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg-state")

	got, err := cli.LedgerPath()
	if err != nil {
		t.Fatalf("LedgerPath: %v", err)
	}
	want := filepath.Join("/xdg-state", "eve-trader", "ledger.json")
	if got != want {
		t.Errorf("LedgerPath() = %q, want %q", got, want)
	}
}

func TestReconcileLedgerFetchesClassifiesAndSaves(t *testing.T) {
	// A stored open-buy lot for 100 units; the live order now shows 60
	// remaining, so 40 filled this run.
	fx := ledgerFixture{
		orders: []map[string]any{
			{"order_id": 1001, "type_id": 34, "volume_total": 100, "volume_remain": 60, "price": 12.5, "is_buy_order": true},
		},
		assets: []map[string]any{
			{"item_id": 1, "type_id": 34, "quantity": 40, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	}
	server, _ := ledgerFixtureServer(t, fx)
	cfg := ledgerTestConfig(t, server.URL)

	writeLedgerFile(t, cfg, []engine.Lot{{
		LotID:                "lot-1",
		TypeID:               34,
		SourceOrderID:        "1001",
		QuantityTotal:        100,
		QuantityAvailable:    0,
		AcquiredAt:           time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:               engine.LotOpenBuy,
		LastSeenVolumeRemain: 100,
	}})

	got, notes, err := cli.ReconcileLedger(t.Context(), cfg)
	if err != nil {
		t.Fatalf("ReconcileLedger: %v", err)
	}
	if len(got) != 1 || got[0].QuantityAvailable != 40 || got[0].LastSeenVolumeRemain != 60 {
		t.Fatalf("got %+v, want a partial fill to 40", got)
	}
	if len(notes) != 1 || notes[0].Kind != engine.NotePartialFill {
		t.Errorf("got notes %+v, want one partial-fill note", notes)
	}

	// The reconciled ledger is persisted, so a restart sees the fill.
	path := filepath.Join(cfg.StateDir, "ledger.json")
	reloaded, err := cli.LoadLedger(path)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(reloaded) != 1 || reloaded[0].QuantityAvailable != 40 {
		t.Errorf("got persisted %+v, want the reconciled lot", reloaded)
	}
}

func TestReconcileLedgerCachesItsESIFetchesAcrossRuns(t *testing.T) {
	fx := ledgerFixture{
		orders: []map[string]any{
			{"order_id": 1001, "type_id": 34, "volume_total": 100, "volume_remain": 100, "is_buy_order": true},
		},
		assets: []map[string]any{},
	}
	server, hits := ledgerFixtureServer(t, fx)
	cfg := ledgerTestConfig(t, server.URL)

	if _, _, err := cli.ReconcileLedger(t.Context(), cfg); err != nil {
		t.Fatalf("ReconcileLedger (first): %v", err)
	}
	if _, _, err := cli.ReconcileLedger(t.Context(), cfg); err != nil {
		t.Fatalf("ReconcileLedger (second): %v", err)
	}

	for _, route := range []string{"orders", "history", "assets"} {
		if got := hits(route); got != 1 {
			t.Errorf("got %d %s requests across two runs, want 1 (cached)", got, route)
		}
	}
}

func TestLedgerCommandPrintsLotsWithoutARecommendRun(t *testing.T) {
	fx := ledgerFixture{
		orders: []map[string]any{},
		assets: []map[string]any{
			{"item_id": 1, "type_id": 34, "quantity": 100, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	}
	server, _ := ledgerFixtureServer(t, fx)
	cfg := ledgerTestConfig(t, server.URL)

	writeLedgerFile(t, cfg, []engine.Lot{{
		LotID:             "seeded-34",
		TypeID:            34,
		SourceOrderID:     engine.SourceOrderSeeded,
		QuantityTotal:     100,
		QuantityAvailable: 100,
		AcquiredAt:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:            engine.LotHeldUnlisted,
	}})

	root := cli.NewRootCmd(cfg)
	var buf strings.Builder
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"ledger"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ledger command: %v", err)
	}

	out := buf.String()
	// The ITEM column shows Tritanium's resolved name, not its bare type id
	// (the batched /universe/names/ lookup, internal/cli/names.go), and
	// ACQUIRED shows the lot's acquisition date — a human-scannable way to
	// tell lots apart that the opaque LOT ID column isn't meant for.
	for _, want := range []string{"seeded-34", "Tritanium", "held-unlisted", "100", "2024-01-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("ledger output %q missing %q", out, want)
		}
	}
	// The inspection command must not run the region-market buy pipeline.
	if strings.Contains(out, "buy_recommendations") {
		t.Errorf("ledger output looks like a recommend result: %q", out)
	}
}

func TestLedgerCommandWarnsVisiblyWhenItClampsToAssets(t *testing.T) {
	// The ledger claims 100 held units but the hangar only confirms 70: the
	// command clamps down and says so (spec §7 step 3).
	fx := ledgerFixture{
		orders: []map[string]any{},
		assets: []map[string]any{
			{"item_id": 1, "type_id": 34, "quantity": 70, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	}
	server, _ := ledgerFixtureServer(t, fx)
	cfg := ledgerTestConfig(t, server.URL)

	writeLedgerFile(t, cfg, []engine.Lot{{
		LotID:             "seeded-34",
		TypeID:            34,
		SourceOrderID:     engine.SourceOrderSeeded,
		QuantityTotal:     100,
		QuantityAvailable: 100,
		AcquiredAt:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:            engine.LotHeldUnlisted,
	}})

	root := cli.NewRootCmd(cfg)
	var out, errOut strings.Builder
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"ledger"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ledger command: %v", err)
	}

	if !strings.Contains(errOut.String(), "warning") {
		t.Errorf("stderr %q missing a drift-clamp warning", errOut.String())
	}
	if !strings.Contains(out.String(), "70") {
		t.Errorf("stdout %q missing the clamped quantity", out.String())
	}
}
