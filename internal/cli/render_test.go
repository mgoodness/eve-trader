package cli_test

import (
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestRenderTableListsFundedBuyRecommendationsInExactISKSortedByExpectedDailyProfit(t *testing.T) {
	result := engine.Result{
		BuyRecommendations: []engine.BuyRecommendation{
			{Name: "1600mm Steel Plates II", BuyPrice: 1720100, SellPrice: 3697900, NetMargin: 0.4791, Units: 17, CommittedCapital: 30623290, ExpectedDailyProfit: 10039780},
			{Name: "Gyrostabilizer II", BuyPrice: 700200, SellPrice: 927200, NetMargin: 0.1847, Units: 110, CommittedCapital: 79707210, ExpectedDailyProfit: 6280853},
		},
	}

	out := cli.RenderTable(result, false)

	plateIdx := strings.Index(out, "1600mm Steel Plates II")
	gyroIdx := strings.Index(out, "Gyrostabilizer II")
	if plateIdx == -1 || gyroIdx == -1 {
		t.Fatalf("got table %q, want both item names present", out)
	}
	if plateIdx > gyroIdx {
		t.Errorf("got 1600mm Steel Plates II after Gyrostabilizer II, want it first (higher expected daily profit)")
	}
	if !strings.Contains(out, "1,720,100") {
		t.Errorf("got table %q, want exact ISK buy price 1,720,100", out)
	}
	if !strings.Contains(out, "3,697,900") {
		t.Errorf("got table %q, want exact ISK sell price 3,697,900", out)
	}
}

func TestRenderTableShowsNotFundedByBudgetSectionWithMarginEDPAndCapitalNeeded(t *testing.T) {
	result := engine.Result{
		Meta: engine.Meta{
			Fees:   engine.Fees{Broker: 0.018},
			Params: engine.RunParams{CaptureRate: 0.20, HorizonDays: 3},
		},
		Unfunded: []engine.BuyRecommendation{
			{Name: "Multispectrum Shield Hardener II", BuyPrice: 1415100, SellPrice: 1994900, NetMargin: 0.2313, ExpectedDailyProfit: 11379279, AverageDailyVolume: 124.9},
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "NOT FUNDED") {
		t.Fatalf("got table %q, want a \"not funded by budget\" section", out)
	}
	if !strings.Contains(out, "Multispectrum Shield Hardener II") {
		t.Errorf("got table %q, want the unfunded item's name", out)
	}
	if !strings.Contains(out, "23.1%") {
		t.Errorf("got table %q, want the unfunded item's net margin", out)
	}
	if !strings.Contains(out, "11,379,279") {
		t.Errorf("got table %q, want the unfunded item's expected daily profit", out)
	}
	// capital needed = (1415100*1.018 + 1994900*0.018) * (0.20*124.9*3 = 74) units.
	if !strings.Contains(out, "needs") {
		t.Errorf("got table %q, want a \"needs ... ISK\" capital figure", out)
	}
}

func TestRenderTableShowsExcludedCountWithoutReasonsByDefault(t *testing.T) {
	result := engine.Result{
		Excluded: []engine.Excluded{
			{TypeID: 34, Name: "Tritanium", Reason: "thin book"},
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "1") {
		t.Errorf("got table %q, want the excluded count (1)", out)
	}
	if strings.Contains(out, "Tritanium") || strings.Contains(out, "thin book") {
		t.Errorf("got table %q, want excluded item names/reasons hidden without --explain", out)
	}
}

func TestRenderTableWithExplainListsExcludedItemsAndReasons(t *testing.T) {
	result := engine.Result{
		Excluded: []engine.Excluded{
			{TypeID: 34, Name: "Tritanium", Reason: "thin book"},
			{TypeID: 35, Name: "Pyerite", Reason: "crossed (2\u03b4 \u2265 spread)"},
		},
	}

	out := cli.RenderTable(result, true)

	if !strings.Contains(out, "Tritanium") || !strings.Contains(out, "thin book") {
		t.Errorf("got table %q, want Tritanium and its reason listed under --explain", out)
	}
	if !strings.Contains(out, "Pyerite") || !strings.Contains(out, "crossed") {
		t.Errorf("got table %q, want Pyerite and its reason listed under --explain", out)
	}
}

func TestRenderTableFallsBackToTheTypeIDWhenNameIsNotYetResolved(t *testing.T) {
	// Name resolution (ESI type IDs -> display names) isn't wired into the
	// whole-universe pipeline yet; the table must still identify every
	// item rather than rendering a blank column.
	result := engine.Result{
		BuyRecommendations: []engine.BuyRecommendation{{TypeID: 11399, ExpectedDailyProfit: 1}},
		Excluded:        []engine.Excluded{{TypeID: 34, Reason: "thin book"}},
	}

	out := cli.RenderTable(result, true)

	if !strings.Contains(out, "11399") {
		t.Errorf("got table %q, want the funded item identified by type_id 11399 when its name is empty", out)
	}
	if !strings.Contains(out, "34") {
		t.Errorf("got table %q, want the excluded item identified by type_id 34 when its name is empty", out)
	}
}

func TestRenderTableRendersAnEmptyResultCleanly(t *testing.T) {
	out := cli.RenderTable(engine.Result{}, true)

	if out == "" {
		t.Fatal("got an empty string, want a clean rendering of a zero-candidate run")
	}
	if strings.Contains(out, "NaN") || strings.Contains(out, "Inf") {
		t.Errorf("got table %q, want no NaN/Inf from dividing by a zero budget", out)
	}
}
