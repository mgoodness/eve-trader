package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// bootstrapFixture is the asset state the fake character routes serve for a
// bootstrap run: untracked Hangar stock at the trade station.
func bootstrapFixture() ledgerFixture {
	return ledgerFixture{
		orders: []map[string]any{},
		assets: []map[string]any{
			{"item_id": 1, "type_id": 34, "quantity": 100, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
			{"item_id": 2, "type_id": 35, "quantity": 5, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	}
}

func TestBootstrapSeedsOnlyConfirmedUntrackedStock(t *testing.T) {
	server, _ := ledgerFixtureServer(t, bootstrapFixture())
	cfg := ledgerTestConfig(t, server.URL)

	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("y\nn\n"))
	root.SetArgs([]string{"bootstrap"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("bootstrap command: %v", err)
	}

	// The listing names both untracked types before asking.
	if !strings.Contains(out.String(), "34") || !strings.Contains(out.String(), "35") {
		t.Errorf("bootstrap output %q does not list both untracked types", out.String())
	}

	lots, err := cli.LoadLedger(filepath.Join(cfg.StateDir, "ledger.json"))
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(lots) != 1 {
		t.Fatalf("got %d lots, want exactly the one confirmed type", len(lots))
	}
	lot := lots[0]
	if lot.TypeID != 34 {
		t.Errorf("TypeID = %d, want 34 (the confirmed type)", lot.TypeID)
	}
	if lot.QuantityTotal != 100 || lot.QuantityAvailable != 100 {
		t.Errorf("quantities = total %d / available %d, want 100/100", lot.QuantityTotal, lot.QuantityAvailable)
	}
	if lot.SourceOrderID != engine.SourceOrderSeeded {
		t.Errorf("SourceOrderID = %q, want %q", lot.SourceOrderID, engine.SourceOrderSeeded)
	}
	if lot.AcquisitionPrice != nil {
		t.Errorf("AcquisitionPrice = %v, want nil for a seeded lot", *lot.AcquisitionPrice)
	}
	if lot.Status != engine.LotHeldUnlisted {
		t.Errorf("Status = %q, want %q", lot.Status, engine.LotHeldUnlisted)
	}
}

func TestBootstrapDeclinedStockIsNotSeeded(t *testing.T) {
	server, _ := ledgerFixtureServer(t, bootstrapFixture())
	cfg := ledgerTestConfig(t, server.URL)

	root := cli.NewRootCmd(cfg)
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	root.SetIn(strings.NewReader("n\nn\n"))
	root.SetArgs([]string{"bootstrap"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("bootstrap command: %v", err)
	}

	path := filepath.Join(cfg.StateDir, "ledger.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("ledger file exists after declining every item (stat err = %v)", err)
	}
}

func TestBootstrapSkipsStockTheLedgerAlreadyAccountsFor(t *testing.T) {
	server, _ := ledgerFixtureServer(t, bootstrapFixture())
	cfg := ledgerTestConfig(t, server.URL)

	writeLedgerFile(t, cfg, []engine.Lot{{
		LotID:             "seeded-34-old",
		TypeID:            34,
		SourceOrderID:     engine.SourceOrderSeeded,
		QuantityTotal:     100,
		QuantityAvailable: 100,
		Status:            engine.LotHeldUnlisted,
	}})

	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("y\n"))
	root.SetArgs([]string{"bootstrap"})

	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("bootstrap command: %v", err)
	}

	lots, err := cli.LoadLedger(filepath.Join(cfg.StateDir, "ledger.json"))
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(lots) != 2 {
		t.Fatalf("got %d lots, want the existing lot plus the newly confirmed type", len(lots))
	}
	var seeded []engine.Lot
	for _, lot := range lots {
		if lot.LotID != "seeded-34-old" {
			seeded = append(seeded, lot)
		}
	}
	// Type 34 was already fully accounted for, so only type 35 was offered
	// and confirmed.
	if len(seeded) != 1 || seeded[0].TypeID != 35 || seeded[0].QuantityTotal != 5 {
		t.Errorf("got newly seeded %+v, want only type 35 with 5 units", seeded)
	}
}

func TestBootstrapSeededLotIsVisibleViaTheLedgerCommand(t *testing.T) {
	server, _ := ledgerFixtureServer(t, bootstrapFixture())
	cfg := ledgerTestConfig(t, server.URL)

	bootstrap := cli.NewRootCmd(cfg)
	bootstrap.SetOut(&strings.Builder{})
	bootstrap.SetErr(&strings.Builder{})
	bootstrap.SetIn(strings.NewReader("y\n"))
	bootstrap.SetArgs([]string{"bootstrap"})
	if err := bootstrap.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("bootstrap command: %v", err)
	}

	lots, err := cli.LoadLedger(filepath.Join(cfg.StateDir, "ledger.json"))
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(lots) != 1 {
		t.Fatalf("got %d lots, want the seeded lot", len(lots))
	}

	// The seeded lot is stock the ledger now knows about, so the inspection
	// command shows it like any other held-unlisted lot.
	ledger := cli.NewRootCmd(cfg)
	var out strings.Builder
	ledger.SetOut(&out)
	ledger.SetErr(&out)
	ledger.SetArgs([]string{"ledger"})
	if err := ledger.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ledger command: %v", err)
	}

	for _, want := range []string{lots[0].LotID, "34", "held-unlisted", "100"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("ledger output %q missing %q", out.String(), want)
		}
	}
}

func TestRecommendNeverSeedsLotsOnItsOwn(t *testing.T) {
	history := map[int32][]map[string]any{11399: historyDays(30, 100, 30000, 15000)}
	server, _ := historyFilteredFixtureServer(t, historyFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.StateDir = t.TempDir()

	root := cli.NewRootCmd(cfg)
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"recommend"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("recommend command: %v", err)
	}

	// Seeding is an explicit pilot action (ADR 0004); a normal recommend run
	// must never create a seeded lot — or a ledger at all on its own.
	path := filepath.Join(cfg.StateDir, "ledger.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("recommend wrote %s (stat err = %v); it must never seed", path, err)
	}
}
