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
// Rens best sell implies, and the decision status. With no unattributed
// fees the break-even range collapses, so low and high agree here; the
// range itself is covered by TestComputePnLForecastRangeAndTargetMargin.
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

	// The default target net margin is 0%, so Target == Break-even.
	if !almostEqual(report.TargetNetMargin, 0) {
		t.Errorf("Report.TargetNetMargin = %v, want 0 (default)", report.TargetNetMargin)
	}

	// Break-even at the 0% net default: cost / (qty x (1 - R_b - R_t)).
	wantBreakEven := 103000.0 / (1000 * (1 - 0.03 - 0.075))
	atTarget, _ := positionByType(report.Positions, 100)
	if !almostEqual(atTarget.BreakEvenHigh, wantBreakEven) {
		t.Errorf("At-target BreakEvenHigh = %v, want %v", atTarget.BreakEvenHigh, wantBreakEven)
	}
	if !almostEqual(atTarget.BreakEvenLow, wantBreakEven) {
		t.Errorf("At-target BreakEvenLow = %v, want %v (no unattributed bucket)", atTarget.BreakEvenLow, wantBreakEven)
	}
	if !almostEqual(atTarget.TargetHigh, wantBreakEven) {
		t.Errorf("At-target TargetHigh = %v, want break-even %v at the 0%% default", atTarget.TargetHigh, wantBreakEven)
	}
	if !almostEqual(atTarget.TargetLow, wantBreakEven) {
		t.Errorf("At-target TargetLow = %v, want break-even %v at the 0%% default", atTarget.TargetLow, wantBreakEven)
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
	if !almostEqual(noMarket.BreakEvenHigh, wantBreakEven) {
		t.Errorf("No-market BreakEvenHigh = %v, want %v", noMarket.BreakEvenHigh, wantBreakEven)
	}
	if noMarket.MarketNetMargin != 0 {
		t.Errorf("No-market MarketNetMargin = %v, want 0 (no fabricated price)", noMarket.MarketNetMargin)
	}
	if noMarket.HasMarket {
		t.Error("No-market position reports HasMarket = true")
	}

	closed, _ := positionByType(report.Positions, 103)
	if !almostEqual(closed.BreakEvenHigh, 0) || !almostEqual(closed.TargetHigh, 0) {
		t.Errorf("Closed forecast = (%v, %v), want zero", closed.BreakEvenHigh, closed.TargetHigh)
	}

	transferred, _ := positionByType(report.Positions, 104)
	if transferred.TransferredQuantity != 1000 {
		t.Errorf("Transferred Quantity = %d, want 1000", transferred.TransferredQuantity)
	}
	if transferred.BreakEvenHigh != 0 {
		t.Errorf("Transferred BreakEvenHigh = %v, want 0", transferred.BreakEvenHigh)
	}
}

// TestComputePnLForecastRangeAndTargetMargin covers the forecast ticket
// (docs/spec/v2.md §4.7): the break-even and target show a low/high range
// (low from confidently-allocated fees, high from sharing the unattributed
// bucket), the view-level target net margin defaults to 0% and raises
// Target above break-even when set, the market-implied margin is reported
// only where the market clears break-even, and the statuses are pinned.
func TestComputePnLForecastRangeAndTargetMargin(t *testing.T) {
	day1 := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

	db := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, db, 123, 0, 0) // R_b 3%, R_t 7.5%

	// Three open positions with different allocated fees (3,000 / 6,000 /
	// 3,000), so the unattributed bucket is shared proportionally to them.
	dbtest.SeedItem(t, db, 200, "Range A")
	seedTx(t, db, 100, day1, 200, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 200, day1, 200, rensLocation, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 2200, 200, false, 120)

	dbtest.SeedItem(t, db, 201, "Range B")
	seedTx(t, db, 101, day1, 201, 1000, 200, true, rensLocation)
	seedOrderSnapshot(t, db, 201, day1, 201, rensLocation, true, 200, 0, 1000, "")
	dbtest.SeedOrder(t, db, 2201, 201, false, 220)

	dbtest.SeedItem(t, db, 202, "Range C")
	seedTx(t, db, 102, day1, 202, 1000, 100, true, rensLocation)
	seedOrderSnapshot(t, db, 202, day1, 202, rensLocation, true, 100, 0, 1000, "")

	// Allocated fees sum to 12,000; the journal carries 1,200 more (an
	// unobserved re-list), which becomes the unattributed bucket.
	seedJournal(t, db,
		[2]any{"brokers_fee", -3000.0},
		[2]any{"brokers_fee", -6000.0},
		[2]any{"brokers_fee", -3000.0},
		[2]any{"brokers_fee", -1200.0},
	)

	report, err := ledger.ComputePnL(t.Context(), db, 123)
	if err != nil {
		t.Fatalf("ComputePnL() error = %v", err)
	}
	if !almostEqual(report.UnattributedFees, 1200) {
		t.Fatalf("Report.UnattributedFees = %v, want 1200", report.UnattributedFees)
	}

	// Position A: allocated fee 3,000, so its share of the 1,200 bucket is
	// 300. Low break-even covers 103,000; high covers 103,300.
	a, _ := positionByType(report.Positions, 200)
	assertAlmost(t, "A BreakEvenLow", a.BreakEvenLow, 103000.0/(1000*(1-0.03-0.075)))
	assertAlmost(t, "A BreakEvenHigh", a.BreakEvenHigh, 103300.0/(1000*(1-0.03-0.075)))
	if !(a.BreakEvenHigh > a.BreakEvenLow) {
		t.Errorf("A BreakEven range = [%v, %v], want high > low", a.BreakEvenLow, a.BreakEvenHigh)
	}
	// Default target (0% net) equals break-even.
	assertAlmost(t, "A TargetLow", a.TargetLow, a.BreakEvenLow)
	assertAlmost(t, "A TargetHigh", a.TargetHigh, a.BreakEvenHigh)
	if a.Status != ledger.StatusAtTarget {
		t.Errorf("A Status = %q, want %q", a.Status, ledger.StatusAtTarget)
	}
	assertAlmost(t, "A MarketNetMargin", a.MarketNetMargin, 4400.0/120000.0)

	// Position B: allocated fee 6,000, share 600. Cost 206,000 -> 206,600.
	b, _ := positionByType(report.Positions, 201)
	assertAlmost(t, "B BreakEvenLow", b.BreakEvenLow, 206000.0/(1000*(1-0.03-0.075)))
	assertAlmost(t, "B BreakEvenHigh", b.BreakEvenHigh, 206600.0/(1000*(1-0.03-0.075)))
	if b.Status != ledger.StatusBelowTarget {
		t.Errorf("B Status = %q, want %q", b.Status, ledger.StatusBelowTarget)
	}
	if b.MarketNetMargin >= 0 {
		t.Errorf("B MarketNetMargin = %v, want <= 0 (market below break-even)", b.MarketNetMargin)
	}

	// Position C has no market: status No market, no fabricated margin, but
	// it still carries its share of the unattributed bucket in the range.
	c, _ := positionByType(report.Positions, 202)
	if c.Status != ledger.StatusNoMarket {
		t.Errorf("C Status = %q, want %q", c.Status, ledger.StatusNoMarket)
	}
	if c.MarketNetMargin != 0 {
		t.Errorf("C MarketNetMargin = %v, want 0", c.MarketNetMargin)
	}
	assertAlmost(t, "C BreakEvenHigh", c.BreakEvenHigh, 103300.0/895.0)

	// A 5% target net margin raises Target above break-even and is carried
	// on the report.
	targeted, err := ledger.ComputePnLForTarget(t.Context(), db, 123, 0.05)
	if err != nil {
		t.Fatalf("ComputePnLForTarget() error = %v", err)
	}
	if !almostEqual(targeted.TargetNetMargin, 0.05) {
		t.Errorf("Report.TargetNetMargin = %v, want 0.05", targeted.TargetNetMargin)
	}
	ta, _ := positionByType(targeted.Positions, 200)
	assertAlmost(t, "A TargetLow at 5%", ta.TargetLow, 103000.0/(1000*(1-0.03-0.075-0.05)))
	assertAlmost(t, "A TargetHigh at 5%", ta.TargetHigh, 103300.0/(1000*(1-0.03-0.075-0.05)))
	if !(ta.TargetHigh > a.TargetHigh) {
		t.Errorf("A TargetHigh at 5%% = %v, want above the 0%% target %v", ta.TargetHigh, a.TargetHigh)
	}
	// Status is measured against break-even (allocated fees), not the view's
	// target margin, per the re-list-forecast addendum: A still clears costs
	// even though its ~3.7% market margin is below the 5% target.
	if ta.Status != ledger.StatusAtTarget {
		t.Errorf("A Status at a 5%% target = %q, want %q (status tracks break-even)", ta.Status, ledger.StatusAtTarget)
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
	if !almostEqual(pos.BreakEvenHigh, wantFloorBreakEven) {
		t.Errorf("Dust BreakEvenHigh = %v, want floor-aware %v", pos.BreakEvenHigh, wantFloorBreakEven)
	}

	// The floor also applies to the target price at a non-zero margin.
	targeted, err := ledger.ComputePnLForTarget(t.Context(), db, 123, 0.05)
	if err != nil {
		t.Fatalf("ComputePnLForTarget() error = %v", err)
	}
	tpos, _ := positionByType(targeted.Positions, 105)
	wantFloorTarget := (1100.0 + 100.0) / (1 * (1 - 0.075 - 0.05))
	if !almostEqual(tpos.TargetHigh, wantFloorTarget) {
		t.Errorf("Dust TargetHigh = %v, want floor-aware %v", tpos.TargetHigh, wantFloorTarget)
	}
}

// assertAlmost is the float assertion the forecast tests share.
func assertAlmost(t *testing.T, name string, got, want float64) {
	t.Helper()
	if !almostEqual(got, want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
