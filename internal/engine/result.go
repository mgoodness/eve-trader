package engine

import "sort"

// NewResult assembles the whole-universe Result (spec §11) from the
// allocation stage's funded and unfunded sets (ticket #22) and the filter
// layer's excluded set (ticket #19, #20), plus the run's Meta (generated
// at, region/station, echoed params, live fees) and the pilot's live
// order limit (ticket #16). It is a pure function: Summary is entirely
// derived from funded/unfunded/excluded and meta.Params.Budget, so it is
// the seam the CLI's table and JSON renderers (ticket #23) both build on.
// A nil budget never divides by zero: BudgetUsed is 0 when Budget is 0.
//
// funded and unfunded arrive from Allocate sorted by capital-efficiency
// density (spec §10 step 1), not by expected daily profit; the output
// contract's default table is sorted by expected daily profit (spec §11),
// so NewResult re-sorts both, descending, before returning them —
// ensuring the JSON contract's recommendations/unfunded arrays carry the
// same invariant the table displays.
func NewResult(funded, unfunded []BuyRecommendation, excluded []Excluded, meta Meta, orderLimit int) Result {
	funded = sortedByExpectedDailyProfit(funded)
	unfunded = sortedByExpectedDailyProfit(unfunded)
	for i := range funded {
		funded[i].Flags = nonNilFlags(funded[i].Flags)
	}
	for i := range unfunded {
		unfunded[i].Flags = nonNilFlags(unfunded[i].Flags)
	}
	if excluded == nil {
		excluded = []Excluded{}
	}

	var committedCapital, expectedDailyProfit float64
	for _, rec := range funded {
		committedCapital += rec.CommittedCapital
		expectedDailyProfit += rec.ExpectedDailyProfit
	}

	var budgetUsed float64
	if meta.Params.Budget > 0 {
		budgetUsed = committedCapital / float64(meta.Params.Budget)
	}

	return Result{
		Meta: meta,
		Summary: Summary{
			Recommendations:     len(funded),
			CommittedCapital:    committedCapital,
			Budget:              meta.Params.Budget,
			BudgetUsed:          budgetUsed,
			OrdersUsed:          len(funded) * ordersPerCandidate,
			OrderLimit:          orderLimit,
			ExpectedDailyProfit: expectedDailyProfit,
			Excluded:            len(excluded),
			Unfunded:            len(unfunded),
		},
		BuyRecommendations: funded,
		Unfunded:        unfunded,
		Excluded:        excluded,
	}
}

// sortedByExpectedDailyProfit returns a copy of recs sorted by expected
// daily profit, descending, never nil (so JSON emits [] rather than null).
func sortedByExpectedDailyProfit(recs []BuyRecommendation) []BuyRecommendation {
	sorted := make([]BuyRecommendation, len(recs))
	copy(sorted, recs)
	sortByExpectedDailyProfitDescending(sorted)
	return sorted
}

// sortByExpectedDailyProfitDescending sorts recs in place, descending by
// expected daily profit, stably. Rank and NewResult share this one
// comparator so the JSON contract's ordering invariant (spec §11) lives in
// exactly one place.
func sortByExpectedDailyProfitDescending(recs []BuyRecommendation) {
	sort.SliceStable(recs, func(i, j int) bool {
		return recs[i].ExpectedDailyProfit > recs[j].ExpectedDailyProfit
	})
}

// nonNilFlags returns flags as a non-nil slice, so a recommendation with no
// pipeline notes marshals as "flags": [] rather than null (spec §11).
func nonNilFlags(flags []string) []string {
	if flags == nil {
		return []string{}
	}
	return flags
}
