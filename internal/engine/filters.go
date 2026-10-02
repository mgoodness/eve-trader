package engine

// GrossMarginCeiling reports the subset of universe whose gross margin —
// (best ask − best bid) / best ask, no δ applied (spec §7 step 5; research
// eveprofits-prior-art.md §2.1) — is at or below ceiling, alongside an
// Excluded record for every candidate dropped for exceeding it. universe
// must already be two-sided (TwoSided): best bid/ask is read directly from
// BuyBook/SellBook, and a candidate missing either side is skipped rather
// than crashing.
func GrossMarginCeiling(universe []CandidateType, ceiling float64) (passed []CandidateType, excluded []Excluded) {
	for _, c := range universe {
		bestBid, haveBid := bestPrice(c.BuyBook)
		bestAsk, haveAsk := bestPrice(c.SellBook)
		if !haveBid || !haveAsk {
			continue
		}

		grossMargin := (bestAsk - bestBid) / bestAsk
		if grossMargin > ceiling {
			excluded = append(excluded, Excluded{
				TypeID: c.TypeID,
				Reason: "gross margin above ceiling: (best ask \u2212 best bid) / best ask exceeds the configured ceiling",
			})
			continue
		}
		passed = append(passed, c)
	}
	return passed, excluded
}

// ThinBook reports the subset of universe whose book is not thin (spec §7
// step 6; research eveprofits-prior-art.md §2.6): at least minOrders buy
// orders within bandPct of the best bid, and at least minOrders sell orders
// within bandPct of the best ask (inclusive of the best order itself on
// each side). A candidate failing either side is dropped with one recorded
// reason naming that side. universe must already be two-sided; a candidate
// missing either side is skipped rather than crashing.
func ThinBook(universe []CandidateType, minOrders int, bandPct float64) (passed []CandidateType, excluded []Excluded) {
	for _, c := range universe {
		bestBid, haveBid := bestPrice(c.BuyBook)
		bestAsk, haveAsk := bestPrice(c.SellBook)
		if !haveBid || !haveAsk {
			continue
		}

		buyFloor := bestBid * (1 - bandPct)
		buysInBand := 0
		for _, o := range c.BuyBook {
			if o.Price >= buyFloor {
				buysInBand++
			}
		}
		if buysInBand < minOrders {
			excluded = append(excluded, Excluded{
				TypeID: c.TypeID,
				Reason: "thin book: fewer than the required orders within band on the buy side",
			})
			continue
		}

		sellCeiling := bestAsk * (1 + bandPct)
		sellsInBand := 0
		for _, o := range c.SellBook {
			if o.Price <= sellCeiling {
				sellsInBand++
			}
		}
		if sellsInBand < minOrders {
			excluded = append(excluded, Excluded{
				TypeID: c.TypeID,
				Reason: "thin book: fewer than the required orders within band on the sell side",
			})
			continue
		}

		passed = append(passed, c)
	}
	return passed, excluded
}

// PricingRule prices the front of queue for every candidate in universe
// (spec §8) and drops the two cases the rule excludes: a crossed tick
// (2δ ≥ spread, Price's ok=false) and a net margin below targetMargin
// (spec §7 step 8). universe must already be two-sided; a candidate missing
// either side is skipped rather than crashing. Candidate names aren't known
// at this layer (Excluded/BuyRecommendation get an empty Name; the caller
// resolves it).
func PricingRule(universe []CandidateType, delta, brokerRate, salesTaxRate, targetMargin float64) (recommendations []BuyRecommendation, excluded []Excluded) {
	for _, c := range universe {
		bestBid, haveBid := bestPrice(c.BuyBook)
		bestAsk, haveAsk := bestPrice(c.SellBook)
		if !haveBid || !haveAsk {
			continue
		}

		rec, ok := Price(c.TypeID, "", bestBid, bestAsk, delta, brokerRate, salesTaxRate)
		if !ok {
			excluded = append(excluded, Excluded{
				TypeID: c.TypeID,
				Reason: "crossed book: 2\u03b4 \u2265 spread",
			})
			continue
		}
		if rec.NetMargin < targetMargin {
			excluded = append(excluded, Excluded{
				TypeID: c.TypeID,
				Reason: "net margin below target",
			})
			continue
		}

		recommendations = append(recommendations, rec)
	}
	return recommendations, excluded
}

// FilterThresholds are the configurable cutoffs for the book-only filters
// (spec §7, §13). The pricing rule's target margin isn't here: it's a
// per-run Params field like Delta and Fees, not a filter threshold.
type FilterThresholds struct {
	GrossMarginCeiling float64
	ThinBookMinOrders  int
	ThinBookBandPct    float64

	// MinHistoryDays, MinLiquidityADV, PriceBandLow, and PriceBandHigh are
	// the history-dependent filters' thresholds (spec \u00a77 steps 3, 4, 7;
	// ticket #20): consumed by FilterByHistory, not FilterBookOnly.
	MinHistoryDays  int
	MinLiquidityADV float64
	PriceBandLow    float64
	PriceBandHigh   float64
}

// FilterBookOnly runs the whole book-only filter stage (spec §7, §6 "History
// funnel"): gross-margin ceiling, thin book, then the pricing rule, in that
// order — the same order as the spec's execution-order note and this
// ticket's acceptance criteria. universe is expected to already be
// two-sided (TwoSided); this function takes no history and makes no I/O, so
// no history call is possible here. Each dropped candidate carries exactly
// one exclusion reason: the first filter it fails, in order, wins, and
// later filters never see it. params.Delta and params.Fees drive the
// pricing rule; params.TargetMargin and params.Filters are this stage's
// thresholds (spec §13).
func FilterBookOnly(universe []CandidateType, params Params) (recommendations []BuyRecommendation, excluded []Excluded) {
	afterCeiling, ceilingExcluded := GrossMarginCeiling(universe, params.Filters.GrossMarginCeiling)
	excluded = append(excluded, ceilingExcluded...)

	afterThinBook, thinBookExcluded := ThinBook(afterCeiling, params.Filters.ThinBookMinOrders, params.Filters.ThinBookBandPct)
	excluded = append(excluded, thinBookExcluded...)

	recs, pricingExcluded := PricingRule(afterThinBook, params.Delta, params.Fees.Broker, params.Fees.SalesTax, params.TargetMargin)
	excluded = append(excluded, pricingExcluded...)

	return recs, excluded
}
