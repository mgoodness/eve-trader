package engine

import (
	"sort"
	"time"
)

// historyWindowDays is the trailing window the history-dependent filters
// read (spec §6 "History funnel", §7 steps 3, 4, 7: "30-day history",
// "30-day ADV", "30-day low/high"). It is a fixed shape of the rule, not a
// per-run threshold, so it is a constant rather than a FilterThresholds
// field.
const historyWindowDays = 30

// HistoryRecord mirrors one daily record of ESI market history (research
// eve-market-mechanics-and-esi.md §6.2: "date, order_count, volume,
// highest, average, lowest"). Only the fields the history-dependent filters
// need are kept; order_count and average aren't used by the filter layer.
type HistoryRecord struct {
	Date    string
	Volume  int64
	Highest float64
	Lowest  float64
}

// CandidateHistory pairs a book-only survivor (already priced by
// FilterBookOnly) with its market history, so the history-dependent filters
// (spec §7 steps 3, 4, 7) can read both the front-of-queue prices
// (Recommendation.BestBid/BestAsk) and the trade record together.
type CandidateHistory struct {
	Recommendation Recommendation
	History        []HistoryRecord
}

// historyLayout is the date format ESI market history uses (research
// eve-market-mechanics-and-esi.md §6.2: "date").
const historyLayout = "2006-01-02"

// historyWindow returns the trailing historyWindowDays calendar days of
// history, sorted ascending by date: the records whose date falls within
// the window ending on the newest record's date (spec §7 steps 3, 4, 7:
// "30-day history", "30-day ADV", "30-day low/high"). A record older than
// that window is not 30-day history even when it is among the last 30
// records — ESI returns up to ~425 days, so record-count trimming would let
// stale volume count toward ADV and would let a long-dead history satisfy
// min-history.
//
// The anchor is the newest record rather than the wall clock: the engine is
// pure and reads no clock, and the window is defined by the data the run
// fetched. ESI's history response is not documented as pre-sorted, so this
// sorts defensively first.
func historyWindow(history []HistoryRecord) []HistoryRecord {
	if len(history) == 0 {
		return nil
	}

	sorted := make([]HistoryRecord, len(history))
	copy(sorted, history)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	anchor, err := time.Parse(historyLayout, sorted[len(sorted)-1].Date)
	if err != nil {
		// A malformed newest date leaves the window undefined; fall back to
		// the trailing record count so behaviour stays deterministic rather
		// than dropping the whole history.
		if len(sorted) > historyWindowDays {
			sorted = sorted[len(sorted)-historyWindowDays:]
		}
		return sorted
	}
	cutoff := anchor.AddDate(0, 0, -(historyWindowDays - 1))

	window := make([]HistoryRecord, 0, len(sorted))
	for _, r := range sorted {
		date, err := time.Parse(historyLayout, r.Date)
		if err != nil {
			continue
		}
		if !date.Before(cutoff) && !date.After(anchor) {
			window = append(window, r)
		}
	}
	return window
}

// distinctHistoryDays counts the days with a record in an already-sorted,
// already-windowed history slice. One ESI record is one day, but counting
// distinct dates (not rows) is what spec §7 step 3's "days of history"
// asks for and is robust to a duplicated record.
func distinctHistoryDays(records []HistoryRecord) int {
	days := 0
	last := ""
	for _, r := range records {
		if r.Date != last {
			days++
			last = r.Date
		}
	}
	return days
}

// MinHistory reports the subset of candidates with at least minDays of
// recent trade history (spec §7 step 3): at least minDays distinct days
// within the trailing 30-day window (historyWindow), not the count of rows
// ESI may return (up to 425 days observed).
func MinHistory(candidates []CandidateHistory, minDays int) (passed []CandidateHistory, excluded []Excluded) {
	for _, c := range candidates {
		if distinctHistoryDays(historyWindow(c.History)) < minDays {
			excluded = append(excluded, Excluded{
				TypeID: c.Recommendation.TypeID,
				Reason: "history too short: fewer than the required days of recent trade history",
			})
			continue
		}
		passed = append(passed, c)
	}
	return passed, excluded
}

// FilterByHistory runs the whole history-dependent filter stage (spec \u00a77
// steps 3, 4, 7; \u00a76 "History funnel"): min history, min liquidity, then
// price band, in that order \u2014 the same order as the spec's numbered filter
// list and this ticket's acceptance criteria. candidates is expected to be
// the book-only survivors (FilterBookOnly's recommendations) paired with
// their fetched history; this function makes no I/O itself \u2014 the caller
// fetches history only for those survivors (spec \u00a76). Each dropped
// candidate carries exactly one exclusion reason: the first filter it fails,
// in order, wins, and later filters never see it.
func FilterByHistory(candidates []CandidateHistory, thresholds FilterThresholds) (recommendations []Recommendation, excluded []Excluded) {
	afterMinHistory, minHistoryExcluded := MinHistory(candidates, thresholds.MinHistoryDays)
	excluded = append(excluded, minHistoryExcluded...)

	afterMinLiquidity, minLiquidityExcluded := MinLiquidity(afterMinHistory, thresholds.MinLiquidityADV)
	excluded = append(excluded, minLiquidityExcluded...)

	afterPriceBand, priceBandExcluded := PriceBand(afterMinLiquidity, thresholds.PriceBandLow, thresholds.PriceBandHigh)
	excluded = append(excluded, priceBandExcluded...)

	for _, c := range afterPriceBand {
		rec := c.Recommendation
		rec.AverageDailyVolume = AverageDailyVolume(c.History)
		recommendations = append(recommendations, rec)
	}
	return recommendations, excluded
}

// AverageDailyVolume computes a candidate's 30-day average daily volume
// (spec §7 step 4, §9 "30-day ADV"): total volume across the trailing
// 30-day window (historyWindow), divided by the fixed 30-day period — not
// by the number of days actually present — so a sparse trading history
// (fewer than 30 days with recorded activity) doesn't inflate the average.
func AverageDailyVolume(history []HistoryRecord) float64 {
	var totalVolume int64
	for _, r := range historyWindow(history) {
		totalVolume += r.Volume
	}
	return float64(totalVolume) / float64(historyWindowDays)
}

// PriceBand reports the subset of candidates whose front-of-queue prices
// (Recommendation.BestBid/BestAsk) sit within the 30-day low/high band
// (spec \u00a77 step 7): best bid must not fall below lowMult times the
// trailing 30-day low (historyWindow's minimum Lowest), and best ask must
// not rise above highMult times the trailing 30-day high (historyWindow's
// maximum Highest). A candidate with no history in the window is skipped
// rather than crashing (min/max over zero records is undefined); MinHistory
// is expected to have already dropped it.
func PriceBand(candidates []CandidateHistory, lowMult, highMult float64) (passed []CandidateHistory, excluded []Excluded) {
	for _, c := range candidates {
		window := historyWindow(c.History)
		if len(window) == 0 {
			continue
		}

		low, high := window[0].Lowest, window[0].Highest
		for _, r := range window[1:] {
			if r.Lowest < low {
				low = r.Lowest
			}
			if r.Highest > high {
				high = r.Highest
			}
		}

		if c.Recommendation.BestBid < lowMult*low {
			excluded = append(excluded, Excluded{
				TypeID: c.Recommendation.TypeID,
				Reason: "price band: best bid is below the configured multiple of the 30-day low",
			})
			continue
		}
		if c.Recommendation.BestAsk > highMult*high {
			excluded = append(excluded, Excluded{
				TypeID: c.Recommendation.TypeID,
				Reason: "price band: best ask is above the configured multiple of the 30-day high",
			})
			continue
		}

		passed = append(passed, c)
	}
	return passed, excluded
}

// MinLiquidity reports the subset of candidates whose 30-day average daily
// volume (ADV) is at or above minADV (spec §7 step 4): total volume across
// the trailing 30-day window (historyWindow), divided by the fixed 30-day
// period — not by the number of days actually present — so a sparse
// trading history (fewer than 30 days with recorded activity) doesn't
// inflate the average.
func MinLiquidity(candidates []CandidateHistory, minADV float64) (passed []CandidateHistory, excluded []Excluded) {
	for _, c := range candidates {
		adv := AverageDailyVolume(c.History)
		if adv < minADV {
			excluded = append(excluded, Excluded{
				TypeID: c.Recommendation.TypeID,
				Reason: "liquidity too low: 30-day average daily volume is below the configured minimum",
			})
			continue
		}
		passed = append(passed, c)
	}
	return passed, excluded
}
