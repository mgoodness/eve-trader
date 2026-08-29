package scanner

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

func TestVolumeWindowAveragesLast7Days(t *testing.T) {
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	var history []esi.MarketHistoryEntry
	// 10 days of history, volumes 1..10 (oldest to newest); only the
	// most recent 7 (volumes 4..10) should count.
	for i := 0; i < 10; i++ {
		history = append(history, esi.MarketHistoryEntry{
			Date:   base.AddDate(0, 0, i),
			Volume: int64(i + 1),
		})
	}

	got := VolumeWindow(history)
	want := (4.0 + 5 + 6 + 7 + 8 + 9 + 10) / 7
	if !almostEqual(got, want) {
		t.Errorf("VolumeWindow() = %v, want %v", got, want)
	}
}

func TestVolumeWindowWithFewerThan7DaysAveragesWhatExists(t *testing.T) {
	history := []esi.MarketHistoryEntry{
		{Volume: 10},
		{Volume: 20},
		{Volume: 30},
	}
	got := VolumeWindow(history)
	want := (10.0 + 20 + 30) / 3
	if !almostEqual(got, want) {
		t.Errorf("VolumeWindow() = %v, want %v", got, want)
	}
}

func TestVolumeWindowEmptyHistory(t *testing.T) {
	if got := VolumeWindow(nil); got != 0 {
		t.Errorf("VolumeWindow(nil) = %v, want 0", got)
	}
}

func TestRankFiltersByMinVolumeAndMinMargin(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 10, AvgDailyVolume: 1000},
		{TypeID: 2, Margin: 2, AvgDailyVolume: 1000}, // margin too low
		{TypeID: 3, Margin: 10, AvgDailyVolume: 5},   // volume too low
		{TypeID: 4, Margin: 20, AvgDailyVolume: 2000},
	}

	got := Rank(opps, Filter{MinVolume: 100, MinMargin: 5}, 50)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2, got %+v", len(got), got)
	}
	if got[0].TypeID != 4 || got[1].TypeID != 1 {
		t.Errorf("got = %+v, want [4, 1] ranked by descending Margin", got)
	}
}

// TestRankFiltersByMinMarkup proves Markup and Margin filter
// independently: an Opportunity must clear both floors to be kept, and
// clearing only one is not enough.
func TestRankFiltersByMinMarkup(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 20, BestBuy: 100, AvgDailyVolume: 1000},  // Markup 0.20: clears both
		{TypeID: 2, Margin: 20, BestBuy: 1000, AvgDailyVolume: 1000}, // Markup 0.02: clears Margin, not Markup
		{TypeID: 3, Margin: 5, BestBuy: 100, AvgDailyVolume: 1000},   // Markup 0.05: clears Markup, not Margin
	}

	got := Rank(opps, Filter{MinMargin: 10, MinMarkup: 0.10}, 50)

	if len(got) != 1 || got[0].TypeID != 1 {
		t.Errorf("got = %+v, want only TypeID 1 (must clear both Margin and Markup floors)", got)
	}
}

// TestRankMinMarkupZeroDoesNotFilter proves the AC: leaving MinMarkup at
// its zero value has no effect beyond what MinMargin/MinVolume already
// decide -- an Opportunity that would have passed before this filter
// existed (Margin already non-negative, so Markup is too) still passes
// with MinMarkup left unset.
func TestRankMinMarkupZeroDoesNotFilter(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 10, BestBuy: 100, AvgDailyVolume: 1000},
		{TypeID: 2, Margin: 0.5, BestBuy: 1000, AvgDailyVolume: 1000}, // small but non-negative Markup
	}

	got := Rank(opps, Filter{}, 50)

	if len(got) != 2 {
		t.Errorf("len(got) = %d, want 2 (MinMarkup unset must not filter beyond what MinMargin/MinVolume already decide)", len(got))
	}
}

func TestRankOrdersByDescendingMargin(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 5},
		{TypeID: 2, Margin: 20},
		{TypeID: 3, Margin: 10},
	}

	got := Rank(opps, Filter{}, 50)

	if len(got) != 3 || got[0].TypeID != 2 || got[1].TypeID != 3 || got[2].TypeID != 1 {
		t.Errorf("got = %+v, want ranked [2, 3, 1]", got)
	}
}

func TestRankRespectsLimit(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 5},
		{TypeID: 2, Margin: 20},
		{TypeID: 3, Margin: 10},
	}

	got := Rank(opps, Filter{}, 2)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].TypeID != 2 || got[1].TypeID != 3 {
		t.Errorf("got = %+v, want the top 2 by Margin", got)
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	opps := []Opportunity{
		{TypeID: 1, Margin: 5},
		{TypeID: 2, Margin: 20},
	}
	original := append([]Opportunity(nil), opps...)

	Rank(opps, Filter{}, 50)

	for i := range opps {
		if opps[i] != original[i] {
			t.Errorf("Rank mutated its input slice: %+v, want unchanged %+v", opps, original)
		}
	}
}

func TestHubsAreIndependent(t *testing.T) {
	// Jita and Rens each have their own RegionID/StationID -- Rank has no
	// hub-crossing logic at all, but this documents the expectation that
	// a caller computes Opportunities per Hub using that Hub's own
	// region/station, never mixing the two (see CONTEXT.md's Hub
	// definition: "never combined").
	if hub.Jita.RegionID == hub.Rens.RegionID {
		t.Fatal("Jita and Rens must have distinct RegionIDs")
	}
	if hub.Jita.StationID == hub.Rens.StationID {
		t.Fatal("Jita and Rens must have distinct StationIDs")
	}
}
