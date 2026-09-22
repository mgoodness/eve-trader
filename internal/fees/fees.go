// Package fees holds eve-trader's broker-fee and sales-tax formulas in
// one place, so the opportunity ranking and the portfolio P/L engine agree
// on them (docs/spec/v2.md §4.3, §4.4, §9). The broker-fee rate folds in
// the station owner's corporation and faction standings additively; both
// are passed in so this package stays a pure formula with no database or
// ESI dependency.
package fees

import "math"

// MinFee is the ISK floor on a single broker fee, placement or modify.
const MinFee = 100.0

// BrokerFeeRate is R_b for a given Broker Relations skill level and the
// character's standings toward the station's owner corporation and that
// corporation's faction (docs/spec/v2.md §9). Standings are on ESI's
// −10…+10 scale; a missing standing is 0.
//
//	R_b = 3% − 0.3%×BrokerRelations − 0.03%×factionStanding − 0.02%×corpStanding
//
// The terms are additive, and the formula is linear: positive standings
// reduce the fee (to EVE's 1% floor at +10/+10), negative standings raise
// it above 3% (research/standings-broker-fees). The per-order 100 ISK
// floor is applied by PlacementFee/ModifyFee, not here.
func BrokerFeeRate(brokerRelationsLevel int, corpStanding, factionStanding float64) float64 {
	return 0.03 - 0.003*float64(brokerRelationsLevel) - 0.0002*corpStanding - 0.0003*factionStanding
}

// SalesTaxRate is R_t for a given Accounting skill level.
func SalesTaxRate(accountingLevel int) float64 {
	return 0.075 * (1 - 0.11*float64(accountingLevel))
}

// PlacementFee is the broker fee charged when an order is placed or
// cancel-and-recreate relisted: max(100 ISK, order_value × R_b).
func PlacementFee(orderValue, brokerFeeRate float64) float64 {
	return math.Max(MinFee, orderValue*brokerFeeRate)
}

// ModifyFee is the broker fee charged for an in-place price modify:
//
//	(1 − (0.50 + 0.06×ABR)) × R_b × NewOrderValue
//	  + R_b × max(NewOrderValue − OldOrderValue, 0)
//
// with a 100 ISK floor. Volume is the volume_remain at modify time, per
// docs/spec/v2.md §4.4.
func ModifyFee(oldOrderValue, newOrderValue, brokerFeeRate, advancedBrokerRelations float64) float64 {
	relist := (1 - (0.50 + 0.06*advancedBrokerRelations)) * brokerFeeRate * newOrderValue
	step := brokerFeeRate * math.Max(newOrderValue-oldOrderValue, 0)
	return math.Max(MinFee, relist+step)
}
