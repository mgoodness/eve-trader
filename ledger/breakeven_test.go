package ledger_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ledger"
)

// TestComputePnLBreakEvenTargetAndStatus pins the per-position figures the
// Portfolio view groups and displays (docs/spec/v2.md §4.7): the break-even
// list price at the zero-net default target, the net margin the current
// Rens best sell implies, and the decision status. These are single
// conservative values here; the low/high range and the URL-carried
// target-margin control arrive with the re-list forecast ticket.
func TestComputePnLBreakEvenTargetAndStatus(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	day5 := time.Date(2024, 1, 5, 10, 0, 0, 0, time.UTC)

	db := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, db, 123, 0, 0) // R_b 3%, R_t 7.5%

	// At target now: 1000 bought at 100 (fee 3000 -> cost 103000), best
	// sell 200. Net proceeds 200000-6000-15000 = 179000, so Unrealized is
	// +76000 and the market implies a 38% net margin.
	dbtest.SeedItem(t, db, 100, "At Target Ore")
	seedTx(t, db, 1, day1, 100, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 100, day1, 100, rensLocation, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 1000, 100, true, 190)
	dbtest.SeedOrder(t, db, 1001, 100, false, 200)

	// Below target: 1000 bought at 100 (cost 103000), best sell 102. Net
	// proceeds 102000-3060-7650 = 91380, so Unrealized is -11710.
	dbtest.SeedItem(t, db, 101, "Below Target Ore")
	seedTx(t, db, 2, day1, 101, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 101, day1, 101, rensLocation, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 1010, 101, true, 101)
	dbtest.SeedOrder(t, db, 1011, 101, false, 102)

	// No market: a held position with no Rens book.
	dbtest.SeedItem(t, db, 102, "No Market Ore")
	seedTx(t, db, 3, day1, 102, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 102, day1, 102, rensLocation, true, 100, 0, 1000, "")

	// Closed: bought 1000 at 100, sold 1000 at 150.
	dbtest.SeedItem(t, db, 103, "Closed Ore")
	seedTx(t, db, 4, day1, 103, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 103, day1, 103, rensLocation, true, 100, 0, 1000, "")
	seedTx(t, db, 5, day2, 103, 1000, 150, false, rensLocation)
	seedOrderSnapshot(t, db, 1030, day2, 103, rensLocation, false, 150, 0, 1000, "")

	// Transferred: bought 1000 at 100, then fully transferred by an
	// item-exchange contract at cost.
	dbtest.SeedItem(t, db, 104, "Transferred Ore")
	seedTx(t, db, 6, day1, 104, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 104, day1, 104, rensLocation, true, 100, 0, 1000, "")
	seedContract(t, db, 900, 123, 0, "item_exchange", 0, day5)
	seedContractItem(t, db, 900, 1, 104, 1000, true)

	report, err := ledger.ComputePnL(t.Context(), db, 123)
	if err != nil {
		t.Fatalf("ComputePnL() error = %v", err)
	}

	wantStatus := map[int]ledger.Status{
		100: ledger.StatusAtTarget,
		101: ledger.StatusBelowTarget,
		102: ledger.StatusNoMarket,
		103: ledger.StatusClosed,
		104: ledger.StatusTransfer,
	}
	for typeID, want := range wantStatus {
		pos, ok := positionByType(report.Positions, typeID)
		if !ok {
			t.Fatalf("no position for type %d; got %+v", typeID, report.Positions)
		}
		if pos.Status != want {
			t.Errorf("Position[%d].Status = %q, want %q", typeID, pos.Status, want)
		}
	}

	// Break-even at the 0% net default: cost / (qty x (1 - R_b - R_t)).
	wantBreakEven := 103000.0 / (1000 * (1 - 0.03 - 0.075))
	atTarget, _ := positionByType(report.Positions, 100)
	if !almostEqual(atTarget.BreakEven, wantBreakEven) {
		t.Errorf("At-target BreakEven = %v, want %v", atTarget.BreakEven, wantBreakEven)
	}
	if !almostEqual(atTarget.Target, wantBreakEven) {
		t.Errorf("At-target Target = %v, want break-even %v at the 0%% default", atTarget.Target, wantBreakEven)
	}
	if !almostEqual(atTarget.MarketNetMargin, 0.38) {
		t.Errorf("At-target MarketNetMargin = %v, want 0.38", atTarget.MarketNetMargin)
	}
	if atTarget.MarketNetMargin <= 0 {
		t.Errorf("At-target MarketNetMargin = %v, want positive", atTarget.MarketNetMargin)
	}

	below, _ := positionByType(report.Positions, 101)
	if below.MarketNetMargin >= 0 {
		t.Errorf("Below-target MarketNetMargin = %v, want <= 0", below.MarketNetMargin)
	}

	noMarket, _ := positionByType(report.Positions, 102)
	if !almostEqual(noMarket.BreakEven, wantBreakEven) {
		t.Errorf("No-market BreakEven = %v, want %v", noMarket.BreakEven, wantBreakEven)
	}
	if noMarket.MarketNetMargin != 0 {
		t.Errorf("No-market MarketNetMargin = %v, want 0 (no fabricated price)", noMarket.MarketNetMargin)
	}
	if noMarket.HasMarket {
		t.Error("No-market position reports HasMarket = true")
	}

	closed, _ := positionByType(report.Positions, 103)
	if !almostEqual(closed.BreakEven, 0) || !almostEqual(closed.Target, 0) {
		t.Errorf("Closed forecast = (%v, %v), want zero", closed.BreakEven, closed.Target)
	}

	transferred, _ := positionByType(report.Positions, 104)
	if transferred.TransferredQuantity != 1000 {
		t.Errorf("Transferred Quantity = %d, want 1000", transferred.TransferredQuantity)
	}
	if transferred.BreakEven != 0 {
		t.Errorf("Transferred BreakEven = %v, want 0", transferred.BreakEven)
	}
}

// TestComputePnLBreakEvenIncludesFeeFloor guards the small-position case:
// the max(100 ISK, value×R_b) broker-fee floor must raise the break-even
// price, not vanish from the forecast (docs/spec/v2.md §4.4, §4.7).
func TestComputePnLBreakEvenIncludesFeeFloor(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	db := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, db, 123, 0, 0) // R_b 3%, R_t 7.5%

	// One unit bought at 1000 with no matching order: the engine estimates
	// the placement fee from the 100 ISK floor, so the cost basis is 1100.
	dbtest.SeedItem(t, db, 105, "Dust")
	seedTx(t, db, 10, day1, 105, 1, 1000, true, rensLocation)

	report, err := ledger.ComputePnL(t.Context(), db, 123)
	if err != nil {
		t.Fatalf("ComputePnL() error = %v", err)
	}
	pos, ok := positionByType(report.Positions, 105)
	if !ok {
		t.Fatalf("no position for type 105; got %+v", report.Positions)
	}

	// Percentage-fee break-even would be 1100/0.895 = 1229.05, at which the
	// broker fee (36.87) is below the 100 ISK floor; the correct break-even
	// solves price×qty − 100 − price×qty×R_t = cost.
	wantFloorBreakEven := (1100.0 + 100.0) / (1 * (1 - 0.075))
	if !almostEqual(pos.BreakEven, wantFloorBreakEven) {
		t.Errorf("Dust BreakEven = %v, want floor-aware %v", pos.BreakEven, wantFloorBreakEven)
	}
}
