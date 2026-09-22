package ranking_test

import (
	"database/sql"
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/internal/fees"
	"github.com/mgoodness/eve-trader/ranking"
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func bound(v float64) *float64 { return &v }

// v1Thresholds reproduces the pre-v1.1 hardcoded thresholds as explicit
// user filters, so formula tests keep their historic fixture expectations.
func v1Thresholds() ranking.Filters {
	return ranking.Filters{MinVolume: bound(10), MinMargin: bound(5)}
}

// seedRealisticItem seeds a candidate that clears every always-on realism
// filter: priced trade-days (the caller supplies at least 7), and two
// orders inside the near-best band on each side, so the item is hidden only
// by the user-filter bounds a test is exercising.
func seedRealisticItem(t *testing.T, sqlDB *sql.DB, typeID int, name string, buy, sell float64, volumes ...int) {
	t.Helper()
	dbtest.SeedItem(t, sqlDB, typeID, name)
	dbtest.SeedBook(t, sqlDB, typeID, buy, sell)
	dbtest.SeedHistory(t, sqlDB, typeID, volumes...)
}

// TestLoadFiltersAndRanksByISKPerDayDescending seeds two above-threshold
// items and two below-threshold items (one under the margin floor, one
// under the volume floor), and asserts Load returns only the
// above-threshold items, ranked ISK/day descending, with the real formula
// values (not hardcoded).
func TestLoadFiltersAndRanksByISKPerDayDescending(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3) // Broker Relations 4, Accounting 3

	// Item A: high EDP. Buy 100 / Sell 120, avg volume 50.
	seedRealisticItem(t, sqlDB, 34, "Tritanium", 100, 120, 50, 50, 50, 50, 50, 50, 50)

	// Item D: lower EDP than A, still above threshold. Buy 50 / Sell 60, avg volume 30.
	seedRealisticItem(t, sqlDB, 35, "Pyerite", 50, 60, 30, 30, 30, 30, 30, 30, 30)

	// Item B: below margin threshold (2.9% < 5%). Buy 100 / Sell 103.
	seedRealisticItem(t, sqlDB, 36, "Below Margin Ore", 100, 103, 100, 100, 100, 100, 100, 100, 100)

	// Item C: below volume threshold (5 < 10). Buy 200 / Sell 240 (healthy margin).
	seedRealisticItem(t, sqlDB, 37, "Below Volume Ore", 200, 240, 5, 5, 5, 5, 5, 5, 5)

	result, err := ranking.Load(t.Context(), sqlDB, v1Thresholds())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := result.Opportunities

	if len(got) != 2 {
		t.Fatalf("Load() returned %d opportunities, want 2 (got %+v)", len(got), got)
	}
	if got[0].Name != "Tritanium" || got[1].Name != "Pyerite" {
		t.Fatalf("Load() order = [%s, %s], want [Tritanium, Pyerite]", got[0].Name, got[1].Name)
	}
	if got[0].ISKPerDay <= got[1].ISKPerDay {
		t.Fatalf("Load() not sorted ISK/day descending: %+v", got)
	}

	// R_b = 3% - 0.3%*4 = 1.8%; R_t = 7.5%*(1-0.11*3) = 5.025%.
	// π = 120 - 100 - (100*0.018) - (120*0.018) - (120*0.05025) = 10.01
	wantProfit := 10.01
	wantGrossMargin := 100.0 / 6.0 // (120-100)/120*100 = 16.666...
	wantNetMargin := wantProfit / 120 * 100
	wantISKPerDay := wantProfit * 50 * ranking.CaptureRate

	tritanium := got[0]
	if !approxEqual(tritanium.ProfitPerUnit, wantProfit) {
		t.Errorf("Tritanium ProfitPerUnit = %v, want %v", tritanium.ProfitPerUnit, wantProfit)
	}
	if !approxEqual(tritanium.GrossMarginPct, wantGrossMargin) {
		t.Errorf("Tritanium GrossMarginPct = %v, want %v", tritanium.GrossMarginPct, wantGrossMargin)
	}
	if !approxEqual(tritanium.NetMarginPct, wantNetMargin) {
		t.Errorf("Tritanium NetMarginPct = %v, want %v", tritanium.NetMarginPct, wantNetMargin)
	}
	if !approxEqual(tritanium.ISKPerDay, wantISKPerDay) {
		t.Errorf("Tritanium ISKPerDay = %v, want %v", tritanium.ISKPerDay, wantISKPerDay)
	}
	if tritanium.Buy != 100 || tritanium.Sell != 120 {
		t.Errorf("Tritanium Buy/Sell = %v/%v, want 100/120", tritanium.Buy, tritanium.Sell)
	}
}

// TestLoadUsesCharacterSkillLevels asserts the same order book produces a
// different ProfitPerUnit under different seeded skill levels -- proof the
// formula is driven by the real character_skill row, not a constant.
func TestLoadUsesCharacterSkillLevels(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0) // unskilled
	seedRealisticItem(t, sqlDB, 34, "Tritanium", 100, 120, 50, 50, 50, 50, 50, 50, 50)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1", len(result.Opportunities))
	}

	// R_b = 3%, R_t = 7.5% at level 0.
	// π = 120 - 100 - (100*0.03) - (120*0.03) - (120*0.075) = 4.4
	wantProfit := 4.4
	if !approxEqual(result.Opportunities[0].ProfitPerUnit, wantProfit) {
		t.Errorf("unskilled ProfitPerUnit = %v, want %v", result.Opportunities[0].ProfitPerUnit, wantProfit)
	}
}

// TestLoadTreatsMissingCharacterSkillRowAsZeroLevels asserts an unseeded
// character_skill table (no row) behaves like level-0 skills rather than
// erroring, mirroring ESIGateway's "missing skill = level 0" convention.
func TestLoadTreatsMissingCharacterSkillRowAsZeroLevels(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedRealisticItem(t, sqlDB, 34, "Tritanium", 100, 120, 50, 50, 50, 50, 50, 50, 50)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1", len(result.Opportunities))
	}

	wantProfit := 4.4
	if !approxEqual(result.Opportunities[0].ProfitPerUnit, wantProfit) {
		t.Errorf("ProfitPerUnit = %v, want %v (level-0 default)", result.Opportunities[0].ProfitPerUnit, wantProfit)
	}
}

// TestLoadExcludesItemsMissingEitherSideOfTheBook asserts an item with
// only buy orders (or only sell orders) on the book -- no spread to
// compute -- is excluded rather than erroring.
func TestLoadExcludesItemsMissingEitherSideOfTheBook(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0)

	dbtest.SeedItem(t, sqlDB, 34, "Buy Only")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, true, 99)
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50, 50, 50, 50, 50, 50)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 0 {
		t.Fatalf("Load() = %+v, want empty (no sell side)", result.Opportunities)
	}
}

// TestLoadRanksOnRegionExtrema seeds a book whose Rens station and another
// Heimatar station disagree and asserts the Opportunity's Buy/Sell are the
// highest region buy and lowest region sell, not the Rens prices
// (docs/adr/0005-heimatar-region-as-pricing-market.md).
func TestLoadRanksOnRegionExtrema(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0)
	dbtest.SeedItem(t, sqlDB, 34, "Region Ore")
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50, 50, 50, 50, 50, 50)

	const otherStation = 60004589
	// Rens book: buy 90, sell 140.
	dbtest.SeedOrderAt(t, sqlDB, 1, 34, dbtest.RensStationID, true, 90)
	dbtest.SeedOrderAt(t, sqlDB, 2, 34, dbtest.RensStationID, true, 89)
	dbtest.SeedOrderAt(t, sqlDB, 3, 34, dbtest.RensStationID, false, 140)
	dbtest.SeedOrderAt(t, sqlDB, 4, 34, dbtest.RensStationID, false, 138)
	// Another station carries the region's best buy and best sell.
	dbtest.SeedOrderAt(t, sqlDB, 5, 34, otherStation, true, 100)
	dbtest.SeedOrderAt(t, sqlDB, 6, 34, otherStation, true, 99)
	dbtest.SeedOrderAt(t, sqlDB, 7, 34, otherStation, false, 130)
	dbtest.SeedOrderAt(t, sqlDB, 8, 34, otherStation, false, 131)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1 (%+v)", len(result.Opportunities), result.Opportunities)
	}
	got := result.Opportunities[0]
	if got.Buy != 100 || got.Sell != 130 {
		t.Errorf("Buy/Sell = %v/%v, want region extrema 100/130", got.Buy, got.Sell)
	}
	// R_b = 3%, R_t = 7.5%: π = 130 - 100 - 3 - 3.9 - 9.75 = 13.35.
	if !approxEqual(got.ProfitPerUnit, 13.35) {
		t.Errorf("ProfitPerUnit = %v, want 13.35 (region extrema)", got.ProfitPerUnit)
	}
}

// TestLoadRealismSingleOrderCheckCountsRegionOrders seeds an item whose
// Rens book has depth on both sides but whose region-best buy is a lone
// order at another station. The single-order-spread filter must count
// region orders near the region best, so the row is hidden even though its
// Rens depth alone would have passed.
func TestLoadRealismSingleOrderCheckCountsRegionOrders(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 0, 0)
	dbtest.SeedItem(t, sqlDB, 35, "Thin Region Ore")
	dbtest.SeedHistory(t, sqlDB, 35, 50, 50, 50, 50, 50, 50, 50)

	const otherStation = 60004589
	// Rens book is two-deep on each side near its own bests...
	dbtest.SeedOrderAt(t, sqlDB, 1, 35, dbtest.RensStationID, true, 100)
	dbtest.SeedOrderAt(t, sqlDB, 2, 35, dbtest.RensStationID, true, 99)
	dbtest.SeedOrderAt(t, sqlDB, 3, 35, dbtest.RensStationID, false, 140)
	dbtest.SeedOrderAt(t, sqlDB, 4, 35, dbtest.RensStationID, false, 138)
	// ...but the region best buy is a single order far above the rest, so
	// the region book's buy side is a thin single-order spread.
	dbtest.SeedOrderAt(t, sqlDB, 5, 35, otherStation, true, 110)
	dbtest.SeedOrderAt(t, sqlDB, 6, 35, otherStation, false, 130)
	dbtest.SeedOrderAt(t, sqlDB, 7, 35, otherStation, false, 131)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 0 {
		t.Fatalf("Load() = %+v, want empty (region buy side is a single order)", result.Opportunities)
	}
	if result.HiddenByRealism != 1 {
		t.Errorf("HiddenByRealism = %d, want 1", result.HiddenByRealism)
	}
}

func TestSortReordersByColumn(t *testing.T) {
	rows := []ranking.Opportunity{
		{Name: "A", Buy: 100, Sell: 120, GrossMarginPct: 15, NetMarginPct: 10, ProfitPerUnit: 5, VolumePerDay: 30, ISKPerDay: 150},
		{Name: "B", Buy: 50, Sell: 60, GrossMarginPct: 15, NetMarginPct: 20, ProfitPerUnit: 8, VolumePerDay: 60, ISKPerDay: 480},
	}

	cases := []struct {
		by        string
		wantFirst string
	}{
		{"buy", "B"},     // 50 < 100
		{"sell", "B"},    // 60 < 120
		{"margin", "B"},  // 20% > 10%
		{"iskunit", "B"}, // 8 > 5
		{"volday", "B"},  // 60 > 30
		{"iskday", "B"},  // 480 > 150
		{"", "B"},        // default falls back to iskday desc
	}
	for _, tc := range cases {
		t.Run(tc.by, func(t *testing.T) {
			got := append([]ranking.Opportunity(nil), rows...)
			ranking.Sort(got, tc.by)
			if got[0].Name != tc.wantFirst {
				t.Errorf("Sort(by=%q) first = %s, want %s", tc.by, got[0].Name, tc.wantFirst)
			}
		})
	}
}

// TestBreakEvenGrossMarginRoundsUpToAvoidFeeNegativeDefaults pins the
// skills-derived minimum-margin default: g* = (2·R_b + R_t) / (1 + R_b),
// rounded up to one decimal so a trade exactly at the default clears fees.
func TestBreakEvenGrossMarginRoundsUpToAvoidFeeNegativeDefaults(t *testing.T) {
	cases := []struct {
		broker, accounting int
		want               float64
	}{
		{5, 5, 6.3},  // g* ≈ 6.2808%
		{4, 3, 8.5},  // g* ≈ 8.4725%
		{0, 0, 13.2}, // g* ≈ 13.1068% at level 0
	}
	for _, tc := range cases {
		got := ranking.BreakEvenGrossMargin(ranking.FeeRates{
			Broker: fees.BrokerFeeRate(tc.broker, 0, 0),
			Tax:    fees.SalesTaxRate(tc.accounting),
		})
		if !approxEqual(got, tc.want) {
			t.Errorf("BreakEvenGrossMargin(Broker %d, Accounting %d) = %v, want %v",
				tc.broker, tc.accounting, got, tc.want)
		}
	}
}

// TestBreakEvenGrossMarginIsAtOrAboveTrueBreakEven guards the rounding
// direction: the rounded default must never fall below the exact break-even
// gross margin.
func TestBreakEvenGrossMarginIsAtOrAboveTrueBreakEven(t *testing.T) {
	for broker := 0; broker <= 5; broker++ {
		for accounting := 0; accounting <= 5; accounting++ {
			rb := fees.BrokerFeeRate(broker, 0, 0)
			rt := fees.SalesTaxRate(accounting)
			exact := (2*rb + rt) / (1 + rb) * 100
			got := ranking.BreakEvenGrossMargin(ranking.FeeRates{Broker: rb, Tax: rt})
			if got < exact {
				t.Errorf("BreakEvenGrossMargin(Broker %d, Accounting %d) = %v below exact %v",
					broker, accounting, got, exact)
			}
			if got-exact >= 0.1 {
				t.Errorf("BreakEvenGrossMargin(Broker %d, Accounting %d) = %v, more than one rounding step above exact %v",
					broker, accounting, got, exact)
			}
		}
	}
}

// TestLoadAppliesStationOwnerStandings pins the standings fast-follow: a
// positive corp and faction standing toward the Rens station owner lowers
// R_b, so the same spread yields a higher per-unit profit than the
// skills-only baseline (docs/spec/v2.md §9).
func TestLoadAppliesStationOwnerStandings(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3) // Broker Relations 4, Accounting 3
	// LoadFeeRates resolves standings for the tracked character, so a token
	// must exist for them to apply.
	dbtest.SeedToken(t, sqlDB, 1, "token-key", "refresh")
	// Rens's owner Brutor Tribe (1000049), faction Minmatar Republic
	// (500002), both at +10.
	dbtest.SeedStationOwner(t, sqlDB, 60004588, 1000049)
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", 10)
	dbtest.SeedStanding(t, sqlDB, 1, 500002, "faction", 10)
	seedRealisticItem(t, sqlDB, 34, "Tritanium", 100, 120, 50, 50, 50, 50, 50, 50, 50)

	result, err := ranking.Load(t.Context(), sqlDB, ranking.Filters{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1", len(result.Opportunities))
	}

	// R_b = 3% - 0.3%*4 - 0.03%*10 - 0.02%*10 = 1.3%; R_t = 5.025%.
	// π = 120 - 100 - (100*0.013) - (120*0.013) - (120*0.05025) = 11.11
	wantProfit := 11.11
	if !approxEqual(result.Opportunities[0].ProfitPerUnit, wantProfit) {
		t.Errorf("standings-adjusted ProfitPerUnit = %v, want %v", result.Opportunities[0].ProfitPerUnit, wantProfit)
	}
}

// TestLoadFeeRatesFallsBackWithoutStandings guards the pessimistic v2
// baseline: an unresolved station owner (no poller run yet) must leave the
// skills-only fee rate in place, not error.
func TestLoadFeeRatesFallsBackWithoutStandings(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3)

	rates, err := ranking.LoadFeeRates(t.Context(), sqlDB)
	if err != nil {
		t.Fatalf("LoadFeeRates() error = %v", err)
	}
	if !approxEqual(rates.Broker, 0.018) || !approxEqual(rates.Tax, 0.05025) {
		t.Errorf("LoadFeeRates() = %+v, want skills-only Broker 0.018, Tax 0.05025", rates)
	}
}
