// Package fees holds eve-trader's broker-fee and sales-tax formulas in
// one place, so the opportunity ranking and the portfolio P/L engine agree
// on them (docs/spec/v2.md §4.3, §4.4). Standings are deliberately omitted
// in v2 (§9): every figure is a pessimistic upper bound for a character
// with non-negative standings.
package fees

import "math"

// MinFee is the ISK floor on a single broker fee, placement or modify.
const MinFee = 100.0

// BrokerFeeRate is R_b for a given Broker Relations skill level.
func BrokerFeeRate(brokerRelationsLevel int) float64 {
	return 0.03 - 0.003*float64(brokerRelationsLevel)
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
