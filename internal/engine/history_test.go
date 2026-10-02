package engine_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// historyCandidate builds a minimal CandidateHistory: a priced recommendation
// for typeID paired with the given daily history records.
func historyCandidate(typeID int32, bestBid, bestAsk float64, history []engine.HistoryRecord) engine.CandidateHistory {
	return engine.CandidateHistory{
		BuyRecommendation: engine.BuyRecommendation{TypeID: typeID, BestBid: bestBid, BestAsk: bestAsk},
		History:           history,
	}
}

// daysOfHistory builds n consecutive daily records ending today, each with
// the given volume and a high/low wide enough not to trip the price band on
// its own.
func daysOfHistory(n int, volume int64) []engine.HistoryRecord {
	records := make([]engine.HistoryRecord, 0, n)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		records = append(records, engine.HistoryRecord{
			Date:    start.AddDate(0, 0, i).Format("2006-01-02"),
			Volume:  volume,
			Highest: 100,
			Lowest:  50,
		})
	}
	return records
}

func TestAverageDailyVolumeIgnoresRecordsOlderThanTheTrailingThirtyDays(t *testing.T) {
	// 30 records spaced two days apart span 60 calendar days; only the 15
	// inside the newest 30-day window count. With volume 10 each, the 30-day
	// ADV is 150/30 = 5 -- not 300/30 = 10, which counting all 30 records
	// would give (spec §7 step 4: the 30-day window, not the last 30 rows).
	records := make([]engine.HistoryRecord, 0, 30)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		records = append(records, engine.HistoryRecord{
			Date:   start.AddDate(0, 0, 2*i).Format("2006-01-02"),
			Volume: 10,
		})
	}

	got := engine.AverageDailyVolume(records)
	if got != 5 {
		t.Errorf("got AverageDailyVolume=%v, want 5 (150 units across the newest 30 calendar days / 30)", got)
	}
}

func TestMinHistoryIgnoresStaleRecordsOutsideTheTrailingThirtyDays(t *testing.T) {
	// 30 records total: 24 stale ones (days 0-23) plus 6 recent ones (days
	// 60-65). Only 6 days fall inside the newest record's trailing 30-day
	// window, so the 7-day minimum is unmet even though there are 30 records.
	var records []engine.HistoryRecord
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 24; i++ {
		records = append(records, engine.HistoryRecord{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Volume: 100})
	}
	for i := 0; i < 6; i++ {
		records = append(records, engine.HistoryRecord{Date: start.AddDate(0, 0, 60+i).Format("2006-01-02"), Volume: 100})
	}
	candidates := []engine.CandidateHistory{historyCandidate(1, 60, 80, records)}

	passed, excluded := engine.MinHistory(candidates, 7)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (only 6 of the newest 30 calendar days have history)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestMinHistoryKeepsAStaleHistoryWhoseRecentRecordsStillMeetTheMinimum(t *testing.T) {
	// 10 stale records (days 0-9) plus 7 recent ones (days 60-66): the recent
	// window still holds 7 days, so the candidate is kept.
	var records []engine.HistoryRecord
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		records = append(records, engine.HistoryRecord{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Volume: 100})
	}
	for i := 0; i < 7; i++ {
		records = append(records, engine.HistoryRecord{Date: start.AddDate(0, 0, 60+i).Format("2006-01-02"), Volume: 100})
	}
	candidates := []engine.CandidateHistory{historyCandidate(1, 60, 80, records)}

	passed, excluded := engine.MinHistory(candidates, 7)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (7 days inside the trailing window)", passed, excluded)
	}
}

func TestMinHistoryKeepsCandidatesWithAtLeastTheMinimumDays(t *testing.T) {
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(7, 100)),
	}

	passed, excluded := engine.MinHistory(candidates, 7)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (7 days meets the 7-day minimum)", passed, excluded)
	}
}

func TestMinHistoryDropsCandidatesWithFewerThanTheMinimumDays(t *testing.T) {
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(6, 100)),
	}

	passed, excluded := engine.MinHistory(candidates, 7)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (6 days is short of the 7-day minimum)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestMinHistoryOnlyCountsTheTrailingThirtyDayWindow(t *testing.T) {
	// 40 days of history, but only the most recent 30 count as "30-day
	// history" (spec §7 step 3); that trailing window still clears 7 days,
	// so the candidate is kept either way -- this proves windowing doesn't
	// wrongly shrink a long history below the minimum.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(40, 100)),
	}

	passed, excluded := engine.MinHistory(candidates, 7)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept", passed, excluded)
	}
}

func TestMinLiquidityKeepsCandidatesAtOrAboveTheMinimumADV(t *testing.T) {
	// 30 days at 20 units/day = ADV 20, exactly at the minimum: kept.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(30, 20)),
	}

	passed, excluded := engine.MinLiquidity(candidates, 20)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (ADV 20 meets the 20 minimum)", passed, excluded)
	}
}

func TestMinLiquidityDropsCandidatesBelowTheMinimumADV(t *testing.T) {
	// 30 days at 19 units/day = ADV 19, below the 20 minimum: dropped.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(30, 19)),
	}

	passed, excluded := engine.MinLiquidity(candidates, 20)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (ADV 19 is below the 20 minimum)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestMinLiquidityDividesByTheFullThirtyDayWindowNotJustDaysWithHistory(t *testing.T) {
	// 10 days at 100 units/day = 1,000 total volume; over a 30-day window
	// that's ADV 33.3 (still above 20), not 100 (which dividing by the 10
	// present days would give) -- a sparse trading history must not inflate
	// the average.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(10, 100)),
	}

	passed, _ := engine.MinLiquidity(candidates, 40)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (1,000/30 = 33.3 ADV is below a 40 minimum)", passed)
	}

	passed, excluded := engine.MinLiquidity(candidates, 30)
	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (1,000/30 = 33.3 ADV clears a 30 minimum)", passed, excluded)
	}
}

func TestPriceBandKeepsCandidatesWithinTheThirtyDayLowHighBand(t *testing.T) {
	// 30-day low/high is 50/100 (daysOfHistory); band is [0.75*50, 1.25*100]
	// = [37.5, 125]. Best bid 60 and best ask 80 both sit inside it.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(7, 100)),
	}

	passed, excluded := engine.PriceBand(candidates, 0.75, 1.25)

	if len(passed) != 1 || len(excluded) != 0 {
		t.Fatalf("got passed=%+v excluded=%+v, want the candidate kept (bid/ask are within the price band)", passed, excluded)
	}
}

func TestPriceBandDropsCandidatesWithBestBidBelowTheLowBandEdge(t *testing.T) {
	// band low edge is 0.75*50 = 37.5; a best bid of 30 is below it.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 30, 80, daysOfHistory(7, 100)),
	}

	passed, excluded := engine.PriceBand(candidates, 0.75, 1.25)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (best bid 30 is below the 37.5 band edge)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func TestPriceBandDropsCandidatesWithBestAskAboveTheHighBandEdge(t *testing.T) {
	// band high edge is 1.25*100 = 125; a best ask of 130 is above it.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 130, daysOfHistory(7, 100)),
	}

	passed, excluded := engine.PriceBand(candidates, 0.75, 1.25)

	if len(passed) != 0 {
		t.Fatalf("got passed=%+v, want none (best ask 130 is above the 125 band edge)", passed)
	}
	if len(excluded) != 1 || excluded[0].TypeID != 1 || excluded[0].Reason == "" {
		t.Fatalf("got excluded=%+v, want one entry for type 1 with a non-empty reason", excluded)
	}
}

func historyThresholds() engine.FilterThresholds {
	return engine.FilterThresholds{
		MinHistoryDays:  7,
		MinLiquidityADV: 20,
		PriceBandLow:    0.75,
		PriceBandHigh:   1.25,
	}
}

func TestFilterByHistoryKeepsACandidateThatClearsAllThreeFilters(t *testing.T) {
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(30, 20)),
	}

	recs, excluded := engine.FilterByHistory(candidates, historyThresholds())

	if len(recs) != 1 || recs[0].TypeID != 1 || len(excluded) != 0 {
		t.Fatalf("got recs=%+v excluded=%+v, want the candidate kept as a recommendation", recs, excluded)
	}
}

func TestFilterByHistoryAttachesAverageDailyVolumeToSurvivors(t *testing.T) {
	// 30 days at 25 units/day = ADV 25.
	candidates := []engine.CandidateHistory{
		historyCandidate(1, 60, 80, daysOfHistory(30, 25)),
	}

	recs, excluded := engine.FilterByHistory(candidates, historyThresholds())

	if len(recs) != 1 || len(excluded) != 0 {
		t.Fatalf("got recs=%+v excluded=%+v, want the candidate kept", recs, excluded)
	}
	if recs[0].AverageDailyVolume != 25 {
		t.Errorf("got AverageDailyVolume=%v, want 25 (750 units over the fixed 30-day period)", recs[0].AverageDailyVolume)
	}
}

func TestFilterByHistoryAppliesTheThreeFiltersInOrderWithOneReasonEach(t *testing.T) {
	// Survives everything.
	survivor := historyCandidate(1, 60, 80, daysOfHistory(30, 20))
	// Fails min history (only 3 days) -- would also fail liquidity and price
	// band if reached, but only the first reason should be recorded.
	shortHistory := historyCandidate(2, 60, 80, daysOfHistory(3, 1))
	// Clears min history, fails min liquidity (ADV 10 < 20).
	thin := historyCandidate(3, 60, 80, daysOfHistory(30, 10))
	// Clears history and liquidity, fails price band (best ask 200 > 125).
	banded := historyCandidate(4, 60, 200, daysOfHistory(30, 20))

	candidates := []engine.CandidateHistory{survivor, shortHistory, thin, banded}

	recs, excluded := engine.FilterByHistory(candidates, historyThresholds())

	if len(recs) != 1 || recs[0].TypeID != 1 {
		t.Fatalf("got recs=%+v, want only type 1", recs)
	}
	if len(excluded) != 3 {
		t.Fatalf("got excluded=%+v, want exactly one reason for each of types 2, 3, 4", excluded)
	}
	reasons := map[int32]string{}
	for _, e := range excluded {
		reasons[e.TypeID] = e.Reason
	}
	if reasons[2] == "" || reasons[3] == "" || reasons[4] == "" {
		t.Fatalf("got reasons=%+v, want a non-empty reason for types 2, 3, and 4", reasons)
	}
	if reasons[2] == reasons[3] || reasons[3] == reasons[4] {
		t.Fatalf("got reasons=%+v, want each candidate to fail a different filter", reasons)
	}
}
