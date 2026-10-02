package engine

import "math"

// BuyRecommendation is a candidate paired with the front-of-queue prices, the
// resulting net margin, and (in later tickets) a unit quantity. Field names
// match the JSON output contract (spec §11).
type BuyRecommendation struct {
	TypeID        int32   `json:"type_id"`
	Name          string  `json:"name"`
	BestBid       float64 `json:"best_bid"`
	BestAsk       float64 `json:"best_ask"`
	Spread        float64 `json:"spread"`
	BuyPrice      float64 `json:"buy_price"`
	SellPrice     float64 `json:"sell_price"`
	NetMargin     float64 `json:"net_margin"`
	ProfitPerUnit float64 `json:"profit_per_unit"`

	// AverageDailyVolume is the trailing 30-day ADV (spec §7 step 4, §9),
	// attached by FilterByHistory once history is available. Zero for a
	// recommendation that never went through the history stage.
	AverageDailyVolume float64 `json:"average_daily_volume"`

	// ExpectedDailyProfit, RoiPerDay, and ExpectedDailyProfitPerOrderSlot
	// are Rank's outputs (spec §9): zero until Rank runs.
	ExpectedDailyProfit             float64 `json:"expected_daily_profit"`
	RoiPerDay                       float64 `json:"roi_per_day"`
	ExpectedDailyProfitPerOrderSlot float64 `json:"expected_daily_profit_per_order_slot"`

	// Units, CommittedCapital, and DaysOfSupply are Allocate's outputs (spec
	// §10): zero until Allocate runs. A candidate Allocate could not fund
	// (the unfunded set) always has Units == 0. The JSON tag stays
	// `days_to_clear` because spec §11 mandates it, but the Go identifier
	// follows CONTEXT.md's "Days of supply".
	Units            int64   `json:"units"`
	CommittedCapital float64 `json:"committed_capital"`
	DaysOfSupply     float64 `json:"days_to_clear"`

	// Flags are pipeline-known notes for this recommendation (spec §11). The
	// only flag v1 sets is FlagPartialFill; the slice is never nil so the
	// JSON contract emits [] rather than null.
	Flags []string `json:"flags"`
}

// FlagPartialFill marks a recommendation whose allocated units were reduced
// below its units cap by the remaining budget (a partial fill, spec §10
// step 4).
const FlagPartialFill = "partial_fill"

// Price computes the front-of-queue recommendation (spec §8) for a single
// candidate: B* = best bid + δ, S* = best ask − δ, and the net margin at the
// given broker and sales-tax rates. It reports ok=false when the tick would
// cross the book (2δ ≥ spread, so B* ≥ S*) — no recommendation is possible.
func Price(typeID int32, name string, bestBid, bestAsk, delta, brokerRate, salesTaxRate float64) (BuyRecommendation, bool) {
	spread := bestAsk - bestBid
	if 2*delta >= spread {
		return BuyRecommendation{}, false
	}

	buyPrice := bestBid + delta
	sellPrice := bestAsk - delta
	// The broker fee is charged on each leg at the minimum of 100 ISK per
	// order (spec §4, §8). Pricing does not know the eventual order size, so
	// it prices the smallest possible order, one unit: each leg is floored at
	// MinBrokerFee, keeping the filter conservative about thin, low-value
	// spreads. Allocate applies the same floor to the real order value.
	brokerBuy := math.Max(brokerRate*buyPrice, MinBrokerFee)
	brokerSell := math.Max(brokerRate*sellPrice, MinBrokerFee)
	netMargin := netMarginFromCost(buyPrice, sellPrice, brokerBuy, brokerSell, salesTaxRate)
	profitPerUnit := netMargin * sellPrice

	return BuyRecommendation{
		TypeID:        typeID,
		Name:          name,
		BestBid:       bestBid,
		BestAsk:       bestAsk,
		Spread:        spread,
		BuyPrice:      buyPrice,
		SellPrice:     sellPrice,
		NetMargin:     netMargin,
		ProfitPerUnit: profitPerUnit,
		Flags:         []string{},
	}, true
}

// netMarginFromCost is the spec §9/§12 net-margin arithmetic in one place:
// given the cost basis B (a front-of-queue buy price for a buy
// recommendation, a lot's acquisition price for a sell recommendation) and
// the front-of-queue sell price S*, it returns
// (S* − B − brokerBuy − brokerSell − tax·S*)/S*, where brokerBuy and
// brokerSell are the broker charges on the two legs. A zero sell price yields
// zero rather than dividing by zero.
func netMarginFromCost(cost, sellPrice, brokerBuy, brokerSell, salesTax float64) float64 {
	if sellPrice == 0 {
		return 0
	}
	return (sellPrice - cost - brokerBuy - brokerSell - salesTax*sellPrice) / sellPrice
}
