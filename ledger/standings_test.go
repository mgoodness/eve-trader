package ledger_test

import (
	"math"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ledger"
)

// TestComputePnLUsesStationOwnerStandings pins the fast-follow on the
// portfolio side: the Rens station owner's corp and faction standings lower
// the broker-fee rate the P/L engine estimates, so the same buy costs less
// (docs/spec/v2.md §9).
func TestComputePnLUsesStationOwnerStandings(t *testing.T) {
	tests := []struct {
		name          string
		seedStandings bool
		wantFee       float64 // buy-side placement fee on 100 × 100 ISK
		wantAvgCost   float64 // (10,000 gross + fee) / 100 units
	}{
		// R_b = 3%: max(100, 10,000×0.03) = 300.
		{"skills only", false, 300, 103},
		// R_b = 3% − 0.02%×10 − 0.03%×10 = 2.5%: max(100, 10,000×0.025) = 250.
		{"owner corp and faction standings", true, 250, 102.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB := dbtest.OpenDB(t)
			dbtest.SeedSkills(t, sqlDB, 1, 0, 0) // level-0 skills
			if tc.seedStandings {
				// Rens's owner Brutor Tribe (1000049), faction Minmatar
				// Republic (500002), each at +10.
				dbtest.SeedStationOwner(t, sqlDB, 60004588, 1000049)
				dbtest.SeedStanding(t, sqlDB, 123, 1000049, "npc_corp", 10)
				dbtest.SeedStanding(t, sqlDB, 123, 500002, "faction", 10)
			}
			seedTx(t, sqlDB, 1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 34, 100, 100, true, rensLocation)

			report, err := ledger.ComputePnL(t.Context(), sqlDB, 123)
			if err != nil {
				t.Fatalf("ComputePnL() error = %v", err)
			}
			if len(report.Positions) != 1 {
				t.Fatalf("ComputePnL() returned %d positions, want 1", len(report.Positions))
			}
			p := report.Positions[0]
			if math.Abs(p.EstimatedFees-tc.wantFee) > 1e-9 {
				t.Errorf("EstimatedFees = %v, want %v", p.EstimatedFees, tc.wantFee)
			}
			if math.Abs(p.AverageCost-tc.wantAvgCost) > 1e-9 {
				t.Errorf("AverageCost = %v, want %v", p.AverageCost, tc.wantAvgCost)
			}
		})
	}
}
