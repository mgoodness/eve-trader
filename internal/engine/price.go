package engine

// Recommendation is a candidate paired with the front-of-queue prices, the
// resulting net margin, and (in later tickets) a unit quantity. Field names
// match the JSON output contract (spec §11).
type Recommendation struct {
	TypeID        int32   `json:"type_id"`
	Name          string  `json:"name"`
	BestBid       float64 `json:"best_bid"`
	BestAsk       float64 `json:"best_ask"`
	Spread        float64 `json:"spread"`
	BuyPrice      float64 `json:"buy_price"`
	SellPrice     float64 `json:"sell_price"`
	NetMargin     float64 `json:"net_margin"`
	ProfitPerUnit float64 `json:"profit_per_unit"`
}

// Price computes the front-of-queue recommendation (spec §8) for a single
// candidate: B* = best bid + δ, S* = best ask − δ, and the net margin at the
// given broker and sales-tax rates. It reports ok=false when the tick would
// cross the book (2δ ≥ spread, so B* ≥ S*) — no recommendation is possible.
func Price(typeID int32, name string, bestBid, bestAsk, delta, brokerRate, salesTaxRate float64) (Recommendation, bool) {
	spread := bestAsk - bestBid
	if 2*delta >= spread {
		return Recommendation{}, false
	}

	buyPrice := bestBid + delta
	sellPrice := bestAsk - delta
	profitPerUnit := sellPrice - buyPrice - brokerRate*buyPrice - brokerRate*sellPrice - salesTaxRate*sellPrice
	netMargin := profitPerUnit / sellPrice

	return Recommendation{
		TypeID:        typeID,
		Name:          name,
		BestBid:       bestBid,
		BestAsk:       bestAsk,
		Spread:        spread,
		BuyPrice:      buyPrice,
		SellPrice:     sellPrice,
		NetMargin:     netMargin,
		ProfitPerUnit: profitPerUnit,
	}, true
}
