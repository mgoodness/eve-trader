package engine

// ordersPerCandidate is the number of concurrent orders one posted buy
// candidate costs (spec §10, §13; decision 3): a single buy order. The sell
// leg is a separate sell recommendation that claims its own slot, so the
// old two-slots-per-candidate assumption no longer holds.
const ordersPerCandidate = 1

// Rank computes each candidate's expected daily profit (spec §9, CONTEXT.md
// "Expected daily profit") and sorts by it, descending. captureRate is the
// configurable flat fraction of 30-day ADV assumed capturable per day (spec
// §13); AverageDailyVolume must already be populated (FilterByHistory sets
// it).
//
//	EDP = net profit per unit × capture rate × 30-day ADV
func Rank(recommendations []BuyRecommendation, captureRate float64) []BuyRecommendation {
	ranked := make([]BuyRecommendation, len(recommendations))
	for i, rec := range recommendations {
		rec.ExpectedDailyProfit = rec.ProfitPerUnit * captureRate * rec.AverageDailyVolume
		rec.RoiPerDay = rec.ProfitPerUnit / rec.BuyPrice
		rec.ExpectedDailyProfitPerOrderSlot = rec.ExpectedDailyProfit / ordersPerCandidate
		ranked[i] = rec
	}

	sortByExpectedDailyProfitDescending(ranked)
	return ranked
}
