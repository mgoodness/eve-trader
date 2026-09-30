package cli_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// TestBuildResultAssemblesTheOutputContractFromTheAllocatedUniverse proves
// BuildResult wires AllocatedUniverse's funded/unfunded/excluded sets and
// the live PilotFacts they were computed with into the stable JSON
// contract's shape (spec §11): a Meta echoing the run's params and live
// fees, and a Summary/Recommendations/Unfunded/Excluded consistent with
// what AllocatedUniverse reported.
func TestBuildResultAssemblesTheOutputContractFromTheAllocatedUniverse(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	result, warnings, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}

	if result.Meta.RegionID != cfg.RegionID || result.Meta.TradeStation != cfg.TradeStationID {
		t.Errorf("got Meta=%+v, want RegionID=%d TradeStation=%d", result.Meta, cfg.RegionID, cfg.TradeStationID)
	}
	if result.Meta.Params.Budget != cfg.Values.Budget {
		t.Errorf("got Params.Budget=%v, want %v", result.Meta.Params.Budget, cfg.Values.Budget)
	}
	// pilotSkills(): Trade 4, Broker Relations 4, Accounting 3, zero
	// standings (spec §4) -> broker 1.8%, sales tax 5.025%.
	if result.Meta.Fees.Broker != 0.018 || result.Meta.Fees.SalesTax != 0.05025 {
		t.Errorf("got Fees=%+v, want broker 0.018 sales tax 0.05025", result.Meta.Fees)
	}
	if result.Meta.Params.Accounting != 3 || result.Meta.Params.BrokerRelations != 4 {
		t.Errorf("got Params.Accounting=%d Params.BrokerRelations=%d, want 3 and 4", result.Meta.Params.Accounting, result.Meta.Params.BrokerRelations)
	}

	if result.Summary.Recommendations+result.Summary.Unfunded+result.Summary.Excluded != 3 {
		t.Fatalf("got summary=%+v, want funded+unfunded+excluded to account for all 3 candidates in the fixture", result.Summary)
	}
	// Tritanium (34) is excluded by the book-only stage.
	if result.Summary.Excluded != 1 {
		t.Errorf("got Excluded=%d, want 1", result.Summary.Excluded)
	}

	for _, rec := range result.Recommendations {
		if rec.RoiPerDay == 0 {
			t.Errorf("got recommendation %+v with zero RoiPerDay, want it populated", rec)
		}
	}
}

// TestBuildResultSplitsEveryTwoSidedTypeAcrossTheThreeGroupsWhenTheBudgetIsTight
// locks the output contract's first invariant (spec §11, §14): a candidate
// that clears the filters but cannot be funded is reported in the unfunded
// group rather than dropped, and funded + unfunded + excluded still accounts
// for every two-sided type. A budget below the minimum order forces both of
// the fixture's survivors out of the funded set.
func TestBuildResultSplitsEveryTwoSidedTypeAcrossTheThreeGroupsWhenTheBudgetIsTight(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 1_000_000 // below the 1M minimum order for both survivors

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	if len(result.Recommendations) != 0 {
		t.Errorf("got %d funded, want 0 with a budget below the minimum order", len(result.Recommendations))
	}
	if len(result.Unfunded) != 2 {
		t.Fatalf("got %d unfunded, want the 2 filtered survivors", len(result.Unfunded))
	}
	if len(result.Excluded) != 1 {
		t.Errorf("got %d excluded, want Tritanium (34)", len(result.Excluded))
	}
	if got := result.Summary.Recommendations + result.Summary.Unfunded + result.Summary.Excluded; got != 3 {
		t.Errorf("got funded+unfunded+excluded=%d, want all 3 two-sided types accounted for", got)
	}
	for _, rec := range result.Unfunded {
		if rec.Units != 0 || rec.CommittedCapital != 0 {
			t.Errorf("got unfunded %+v, want zero units and zero committed capital", rec)
		}
		needed := engine.CapitalNeeded(rec, result.Meta.Fees.Broker, result.Meta.Params.CaptureRate, result.Meta.Params.HorizonDays)
		if needed <= 0 {
			t.Errorf("got unfunded %+v with no capital-needed figure, want a positive one", rec)
		}
	}
}
