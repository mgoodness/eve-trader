package engine

import (
	"fmt"
	"math"
	"sort"
)

// AllocationParams are the run's configurable allocation inputs (spec §10,
// §13): the pilot's self-reported budget, the skill-derived order limit
// (ticket #16, engine.OrderLimit), the minimum committed capital worth
// posting, the flat capture rate (spec §9), the capture horizon in days,
// the broker fee rate committed capital is priced at, and the resources
// the pilot's pre-existing open orders already reserve (spec §13 steps 1–2;
// ticket #49).
type AllocationParams struct {
	Budget      int64
	OrderLimit  int
	MinOrder    int64
	CaptureRate float64
	HorizonDays int
	Broker      float64

	// ReservedSlots is the number of order-limit slots every currently-open
	// order (buy or sell, any origin) occupies before this run allocates
	// anything (spec §13 step 1).
	ReservedSlots int

	// ReservedBudget is the ISK already escrowed against open buy orders,
	// sum of price × volume_remain; open sell orders carry no escrow, and an
	// unknown-outcome lot is absent from the orders snapshot so it reserves
	// nothing (spec §13 step 2; ADR 0003).
	ReservedBudget float64
}

// AllocateWithSells allocates sell recommendations first, then new buy
// recommendations, against the resources left after ReservedSlots and
// ReservedBudget (spec §13; ADR 0007). Sells cost one order slot each and no
// budget — recovering already-spent capital takes priority over deploying
// fresh capital. A sell inside the headroom is funded in its existing order
// (the sell plan's worst-margin-shortfall sort, spec §11); once the slots
// run out, the excess land in Pending with reason order-limit-exhausted
// rather than being dropped. Buy allocation then runs, unchanged, on
// whatever slots and budget remain.
func AllocateWithSells(sells []SellRecommendation, buys []BuyRecommendation, params AllocationParams) (fundedSells []SellRecommendation, pendingSells []Pending, fundedBuys, unfundedBuys []BuyRecommendation) {
	availableSlots := params.OrderLimit - params.ReservedSlots
	if availableSlots < 0 {
		availableSlots = 0
	}

	for _, sell := range sells {
		if len(fundedSells) >= availableSlots {
			pendingSells = append(pendingSells, Pending{
				TypeID:   sell.TypeID,
				Name:     sell.Name,
				Reason:   PendingOrderLimit,
				Quantity: sell.Quantity,
				Detail: fmt.Sprintf("no free order-limit slot: %d of %d slots reserved before this sell (orders already open plus higher-priority sells)",
					params.ReservedSlots+len(fundedSells), params.OrderLimit),
			})
			continue
		}
		fundedSells = append(fundedSells, sell)
	}

	// The funded sells hold their slots against the buy allocation too: buy
	// params reserve them on top of the pre-existing orders.
	buyParams := params
	buyParams.ReservedSlots = params.ReservedSlots + len(fundedSells)
	fundedBuys, unfundedBuys = Allocate(buys, buyParams)
	return fundedSells, pendingSells, fundedBuys, unfundedBuys
}

// Allocate selects which ranked candidates to post and how many units each
// (spec §10, CONTEXT.md "Allocation"), under the budget and the
// skill-derived order limit less the resources already reserved by
// pre-existing orders (spec §13 steps 1–2). Each posted candidate costs one
// order slot (decision 3: a single order per recommendation). ranked's order
// is preserved for funded and unfunded; callers wanting the funded set
// sorted by expected daily profit for display (spec §11) sort ranked before
// calling Allocate, or re-sort the result.
func Allocate(ranked []BuyRecommendation, params AllocationParams) (funded, unfunded []BuyRecommendation) {
	byDensity := make([]BuyRecommendation, len(ranked))
	copy(byDensity, ranked)
	sort.SliceStable(byDensity, func(i, j int) bool {
		return density(byDensity[i], params) > density(byDensity[j], params)
	})

	remaining := float64(params.Budget) - params.ReservedBudget
	ordersUsed := params.ReservedSlots

	for _, rec := range byDensity {
		rec.Units = 0
		rec.Flags = []string{}

		cap := unitsCap(rec, params.CaptureRate, params.HorizonDays)
		perUnit := committedCapitalPerUnit(rec, params.Broker, cap)

		if ordersPerCandidate+ordersUsed > params.OrderLimit || cap <= 0 || perUnit <= 0 {
			unfunded = append(unfunded, rec)
			continue
		}

		units := cap
		if committedCapitalAt(rec, params.Broker, cap) > remaining {
			units = affordableUnits(remaining, rec, params.Broker, cap)
		}
		spent := committedCapitalAt(rec, params.Broker, units)

		if spent < float64(params.MinOrder) {
			unfunded = append(unfunded, rec)
			continue
		}

		if units < cap {
			rec.Flags = append(rec.Flags, FlagPartialFill)
		}
		rec.Units = units
		rec.CommittedCapital = spent
		rec.DaysOfSupply = float64(units) / (params.CaptureRate * rec.AverageDailyVolume)

		remaining -= spent
		ordersUsed += ordersPerCandidate
		funded = append(funded, rec)
	}

	return funded, unfunded
}

// CapitalNeeded reports the committed capital a candidate would need to be
// funded at its full units cap (spec §10 steps 2–3): the same per-unit
// capital and units-cap formula Allocate applies internally, exposed so
// the output contract's "not funded by budget" section (spec §11) can
// show what an unfunded candidate needs, without Allocate itself ever
// setting Units or CommittedCapital on a candidate it did not fund.
func CapitalNeeded(rec BuyRecommendation, broker, captureRate float64, horizonDays int) float64 {
	return committedCapitalAt(rec, broker, unitsCap(rec, captureRate, horizonDays))
}

// unitsCap is the most units a candidate may be posted at (spec §10 step
// 2): capture rate × 30-day ADV × horizon.
func unitsCap(rec BuyRecommendation, captureRate float64, horizonDays int) int64 {
	return int64(captureRate * rec.AverageDailyVolume * float64(horizonDays))
}

// committedCapitalAt is the ISK a posted order of units ties up (CONTEXT.md
// "Committed capital", spec §10 step 3): units × B* escrow plus the broker
// fee on each leg, each floored at MinBrokerFee per order (spec §4, §8).
// Sales tax is netted from sale proceeds, not committed.
func committedCapitalAt(rec BuyRecommendation, brokerRate float64, units int64) float64 {
	if units <= 0 {
		return 0
	}
	escrow := float64(units) * rec.BuyPrice
	buyFee := math.Max(brokerRate*escrow, MinBrokerFee)
	sellFee := math.Max(brokerRate*float64(units)*rec.SellPrice, MinBrokerFee)
	return escrow + buyFee + sellFee
}

// committedCapitalPerUnit is committedCapitalAt amortised over units, for
// ranking (density) and per-unit comparisons. A non-positive units is
// treated as one unit so the result is always a defined per-unit figure.
func committedCapitalPerUnit(rec BuyRecommendation, brokerRate float64, units int64) float64 {
	if units <= 0 {
		units = 1
	}
	return committedCapitalAt(rec, brokerRate, units) / float64(units)
}

// affordableUnits returns the largest unit count up to cap whose committed
// capital fits remaining (spec §10 step 4: partially fill rather than
// skip). committedCapitalAt is monotonically non-decreasing in units, so
// the affordability predicate is monotone and a binary search is exact.
func affordableUnits(remaining float64, rec BuyRecommendation, brokerRate float64, cap int64) int64 {
	lo, hi := int64(0), cap
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if committedCapitalAt(rec, brokerRate, mid) <= remaining {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// density is a candidate's expected daily profit per ISK of committed
// capital (spec §10 step 1) — the fractional-knapsack sort key: expected
// daily profit is already a rate independent of how many units are posted
// (spec §9), so dividing by the capital one unit ties up ranks candidates
// by how hard each ISK of budget works, regardless of how much budget is
// actually available. The per-unit capital is taken at the units cap, where
// the 100 ISK per-order broker floor is most amortised. A non-positive
// per-unit capital ranks last, not first: it cannot be financed at all.
func density(rec BuyRecommendation, params AllocationParams) float64 {
	units := unitsCap(rec, params.CaptureRate, params.HorizonDays)
	capitalPerUnit := committedCapitalPerUnit(rec, params.Broker, units)
	if capitalPerUnit <= 0 {
		return -1
	}
	return rec.ExpectedDailyProfit / capitalPerUnit
}
