package ledger_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ledger"
)

// TestComputePnLRelistGain pins the raise-only re-list gain (docs/spec/v2.md
// §4.8): a resting sell order priced below the market's best sell nets
// (P_new - P_old) x volume_remain x (1 - R_t) after the in-place modify fee,
// and only positive nets surface. The character's own orders are excluded
// from the best sell, so raising to the next real price level is a gain even
// when the character currently holds the book's best sell.
func TestComputePnLRelistGain(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	db := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, db, 123, 0, 0) // R_b 3%, R_t 7.5%, ABR 0

	// Worth moving: the character's sell at 100 sits below the market's best
	// sell among others at 120. The fee is the in-place modify on
	// 100,000 -> 120,000 at R_b 3% / ABR 0:
	//   0.5 x 0.03 x 120,000 + 0.03 x 20,000 = 2,400.
	// net = 20,000 x 0.925 - 2,400 = 16,100.
	dbtest.SeedItem(t, db, 301, "Worth Moving Ore")
	seedOrderSnapshot(t, db, 7001, day1, 301, rensLocation, false, 100, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 7001, 301, false, 100) // character's own order, in the public book
	dbtest.SeedOrder(t, db, 9001, 301, false, 120) // the next real best sell
	dbtest.SeedOrder(t, db, 9002, 301, false, 125)

	// Buy orders never surface: raising a bid costs more.
	dbtest.SeedItem(t, db, 302, "Buy Ore")
	seedOrderSnapshot(t, db, 7002, day1, 302, rensLocation, true, 90, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 9003, 302, false, 120)

	// Undercut (above the market's best sell) yields no raise-only gain.
	dbtest.SeedItem(t, db, 303, "Undercut Ore")
	seedOrderSnapshot(t, db, 7003, day1, 303, rensLocation, false, 130, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 9004, 303, false, 120)

	// Already at the best sell: no move.
	dbtest.SeedItem(t, db, 304, "At Best Ore")
	seedOrderSnapshot(t, db, 7004, day1, 304, rensLocation, false, 120, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 9005, 304, false, 120)

	// The modify fee's 100 ISK floor swamps a one-unit move, so a positive
	// price gap is not enough for a positive net.
	dbtest.SeedItem(t, db, 305, "Fee Floor Ore")
	seedOrderSnapshot(t, db, 7005, day1, 305, rensLocation, false, 119.99, 1, 1, "")
	dbtest.SeedOrder(t, db, 9006, 305, false, 120)

	// A cancelled/zero-fill order is not resting.
	dbtest.SeedItem(t, db, 306, "Closed Ore")
	seedOrderSnapshot(t, db, 7006, day1, 306, rensLocation, false, 100, 0, 1000, "cancelled")
	dbtest.SeedOrder(t, db, 9007, 306, false, 120)

	report, err := ledger.ComputePnL(t.Context(), db, 123)
	if err != nil {
		t.Fatalf("ComputePnL() error = %v", err)
	}

	if len(report.RelistGains) != 1 {
		t.Fatalf("Report.RelistGains len = %d, want 1; got %+v", len(report.RelistGains), report.RelistGains)
	}
	g := report.RelistGains[0]
	if g.OrderID != 7001 {
		t.Errorf("RelistGain.OrderID = %d, want 7001", g.OrderID)
	}
	if g.TypeID != 301 || g.Name != "Worth Moving Ore" || g.LocationID != rensLocation {
		t.Errorf("RelistGain identity = (%d, %q, %d), want (301, %q, %d)", g.TypeID, g.Name, g.LocationID, "Worth Moving Ore", rensLocation)
	}
	if g.VolumeRemain != 1000 {
		t.Errorf("RelistGain.VolumeRemain = %d, want 1000", g.VolumeRemain)
	}
	assertAlmost(t, "RelistGain.OldPrice", g.OldPrice, 100)
	assertAlmost(t, "RelistGain.NewPrice", g.NewPrice, 120)
	assertAlmost(t, "RelistGain.NetGain", g.NetGain, 16100)
}

// TestComputePnLRelistGainUsesModifyFeeSkill pins the in-place modify fee's
// Advanced Broker Relations discount: at ABR 5 the relist fraction is 0.20
// instead of 0.50, so the same 100 -> 120 move on 1,000 units nets
// 20,000 x 0.925 - (0.20 x 0.03 x 120,000 + 0.03 x 20,000) = 17,180.
func TestComputePnLRelistGainUsesModifyFeeSkill(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	db := dbtest.OpenDB(t)
	dbtest.SeedSkillsWithAdvancedBrokerRelations(t, db, 123, 0, 0, 5)

	dbtest.SeedItem(t, db, 310, "ABR Ore")
	seedOrderSnapshot(t, db, 7010, day1, 310, rensLocation, false, 100, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 9010, 310, false, 120)

	report, err := ledger.ComputePnL(t.Context(), db, 123)
	if err != nil {
		t.Fatalf("ComputePnL() error = %v", err)
	}
	if len(report.RelistGains) != 1 {
		t.Fatalf("Report.RelistGains len = %d, want 1; got %+v", len(report.RelistGains), report.RelistGains)
	}
	assertAlmost(t, "RelistGain.NetGain at ABR 5", report.RelistGains[0].NetGain, 17180)
}
