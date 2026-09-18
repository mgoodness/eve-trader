package ranking_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ranking"
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 0.001 }

// TestLoadFiltersAndRanksByISKPerDayDescending seeds two above-threshold
// items and two below-threshold items (one under the margin floor, one
// under the volume floor), and asserts Load returns only the
// above-threshold items, ranked ISK/day descending, with the real formula
// values (not hardcoded).
func TestLoadFiltersAndRanksByISKPerDayDescending(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3) // Broker Relations 4, Accounting 3

	// Item A: high EDP. Buy 100 / Sell 120, avg volume 50.
	dbtest.SeedItem(t, sqlDB, 34, "Tritanium")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, false, 120)
	dbtest.SeedHistory(t, sqlDB, 34, 40, 60) // avg 50

	// Item D: lower EDP than A, still above threshold. Buy 50 / Sell 60, avg volume 30.
	dbtest.SeedItem(t, sqlDB, 35, "Pyerite")
	dbtest.SeedOrder(t, sqlDB, 3, 35, true, 50)
	dbtest.SeedOrder(t, sqlDB, 4, 35, false, 60)
	dbtest.SeedHistory(t, sqlDB, 35, 20, 40) // avg 30

	// Item B: below margin threshold (2.9% < 5%). Buy 100 / Sell 103.
	dbtest.SeedItem(t, sqlDB, 36, "Below Margin Ore")
	dbtest.SeedOrder(t, sqlDB, 5, 36, true, 100)
	dbtest.SeedOrder(t, sqlDB, 6, 36, false, 103)
	dbtest.SeedHistory(t, sqlDB, 36, 100, 100) // avg 100, plenty of volume

	// Item C: below volume threshold (5 < 10). Buy 200 / Sell 240 (healthy margin).
	dbtest.SeedItem(t, sqlDB, 37, "Below Volume Ore")
	dbtest.SeedOrder(t, sqlDB, 7, 37, true, 200)
	dbtest.SeedOrder(t, sqlDB, 8, 37, false, 240)
	dbtest.SeedHistory(t, sqlDB, 37, 5, 5) // avg 5

	got, err := ranking.Load(t.Context(), sqlDB)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

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

	dbtest.SeedItem(t, sqlDB, 34, "Tritanium")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, false, 120)
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50)

	got, err := ranking.Load(t.Context(), sqlDB)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1", len(got))
	}

	// R_b = 3%, R_t = 7.5% at level 0.
	// π = 120 - 100 - (100*0.03) - (120*0.03) - (120*0.075) = 4.4
	wantProfit := 4.4
	if !approxEqual(got[0].ProfitPerUnit, wantProfit) {
		t.Errorf("unskilled ProfitPerUnit = %v, want %v", got[0].ProfitPerUnit, wantProfit)
	}
}

// TestLoadTreatsMissingCharacterSkillRowAsZeroLevels asserts an unseeded
// character_skill table (no row) behaves like level-0 skills rather than
// erroring, mirroring ESIGateway's "missing skill = level 0" convention.
func TestLoadTreatsMissingCharacterSkillRowAsZeroLevels(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)

	dbtest.SeedItem(t, sqlDB, 34, "Tritanium")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, false, 120)
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50)

	got, err := ranking.Load(t.Context(), sqlDB)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Load() returned %d opportunities, want 1", len(got))
	}

	wantProfit := 4.4
	if !approxEqual(got[0].ProfitPerUnit, wantProfit) {
		t.Errorf("ProfitPerUnit = %v, want %v (level-0 default)", got[0].ProfitPerUnit, wantProfit)
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
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50)

	got, err := ranking.Load(t.Context(), sqlDB)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load() = %+v, want empty (no sell side)", got)
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
