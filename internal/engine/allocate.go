package engine

import "sort"

// AllocationParams are the run's configurable allocation inputs (spec §10,
// §13): the pilot's self-reported budget, the skill-derived order limit
// (ticket #16, engine.OrderLimit), the minimum committed capital worth
// posting, the flat capture rate (spec §9), the capture horizon in days,
// and the broker fee rate committed capital is priced at.
type AllocationParams struct {
	Budget      int64
	OrderLimit  int
	MinOrder    int64
	CaptureRate float64
	HorizonDays int
	Broker      float64
}

// Allocate selects which ranked candidates to post and how many units each
// (spec §10, CONTEXT.md "Allocation"), under the budget and the
// skill-derived order limit — each posted candidate costs two order slots,
// a buy and a sell. ranked's order is preserved for funded and unfunded;
// callers wanting the funded set sorted by expected daily profit for
// display (spec §11) sort ranked before calling Allocate, or re-sort the
// result.
func Allocate(ranked []Recommendation, params AllocationParams) (funded, unfunded []Recommendation) {
	byDensity := make([]Recommendation, len(ranked))
	copy(byDensity, ranked)
	sort.SliceStable(byDensity, func(i, j int) bool {
		return density(byDensity[i], params.Broker) > density(byDensity[j], params.Broker)
	})

	remaining := float64(params.Budget)
	ordersUsed := 0

	for _, rec := range byDensity {
		rec.Units = 0

		unitsCap := int64(params.CaptureRate * rec.AverageDailyVolume * float64(params.HorizonDays))
		capitalPerUnit := rec.BuyPrice*(1+params.Broker) + rec.SellPrice*params.Broker

		if ordersPerCandidate+ordersUsed > params.OrderLimit || unitsCap <= 0 || capitalPerUnit <= 0 {
			unfunded = append(unfunded, rec)
			continue
		}

		units := unitsCap
		maxCapital := capitalPerUnit * float64(unitsCap)
		if remaining < maxCapital {
			units = int64(remaining / capitalPerUnit)
		}
		spent := capitalPerUnit * float64(units)

		if spent < float64(params.MinOrder) {
			unfunded = append(unfunded, rec)
			continue
		}

		rec.Units = units
		rec.CommittedCapital = spent
		rec.DaysToClear = float64(units) / (params.CaptureRate * rec.AverageDailyVolume)

		remaining -= spent
		ordersUsed += ordersPerCandidate
		funded = append(funded, rec)
	}

	return funded, unfunded
}

// density is a candidate's expected daily profit per ISK of committed
// capital (spec §10 step 1) — the fractional-knapsack sort key: expected
// daily profit is already a rate independent of how many units are posted
// (spec §9), so dividing by the capital one unit ties up ranks candidates
// by how hard each ISK of budget works, regardless of how much budget is
// actually available. Zero committed capital per unit (a zero buy price)
// ranks last, not first: it cannot be financed at all.
func density(rec Recommendation, broker float64) float64 {
	capitalPerUnit := rec.BuyPrice*(1+broker) + rec.SellPrice*broker
	if capitalPerUnit <= 0 {
		return -1
	}
	return rec.ExpectedDailyProfit / capitalPerUnit
}
