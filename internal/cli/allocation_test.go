package cli_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
)

func TestAllocatedUniverseFundsSurvivorsWithinBudgetAndOrderLimit(t *testing.T) {
	history := map[int32][]map[string]any{
		// Morphite: profit/unit \u2248 3693.605, ADV 100.
		11399: historyDays(30, 100, 30000, 15000),
		// Type 40: profit/unit \u2248 650.525, ADV 1000, ranked ahead of
		// Morphite by expected daily profit -- see ranking_test.go.
		40: historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000
	cfg.Values.MinOrder = 1_000_000

	funded, unfunded, excluded, _, _, err := cli.AllocatedUniverse(t.Context(), cfg)
	if err != nil {
		t.Fatalf("AllocatedUniverse: %v", err)
	}

	// Tritanium (34) is still excluded by the book-only stage, unchanged
	// by allocation.
	if len(excluded) != 1 || excluded[0].TypeID != 34 {
		t.Fatalf("got excluded=%+v, want exactly one entry for Tritanium (34)", excluded)
	}

	if len(funded)+len(unfunded) != 2 {
		t.Fatalf("got funded=%+v unfunded=%+v, want both two-sided survivors accounted for", funded, unfunded)
	}
	for _, rec := range funded {
		if rec.Units <= 0 {
			t.Errorf("got funded candidate %+v with Units<=0, want a funded candidate to have units", rec)
		}
		if rec.CommittedCapital > float64(cfg.Values.Budget) {
			t.Errorf("got CommittedCapital=%v, want it to stay within the %v budget", rec.CommittedCapital, cfg.Values.Budget)
		}
	}
	for _, rec := range unfunded {
		if rec.Units != 0 {
			t.Errorf("got unfunded candidate %+v with nonzero Units, want zero", rec)
		}
	}
}
