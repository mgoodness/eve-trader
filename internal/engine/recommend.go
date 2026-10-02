package engine

import "time"

// Fees are the pilot's fee rates, read live from ESI skills and standings
// (spec §4; ticket #16). Broker is charged on both legs of a round trip;
// SalesTax on the sell leg only (spec §4).
type Fees struct {
	Broker   float64 `json:"broker"`
	SalesTax float64 `json:"sales_tax"`
}

// Meta describes the run that produced a Result (spec §11).
type Meta struct {
	GeneratedAt  time.Time `json:"generated_at"`
	RegionID     int32     `json:"region_id"`
	TradeStation int64     `json:"trade_station"`
	Params       RunParams `json:"params"`
	Fees         Fees      `json:"fees"`
}

// RunParams are the run's configurable inputs (spec §13), echoed back in
// the JSON contract (spec §11) so an adapter can audit or reproduce a run
// without re-reading config.toml or ESI. Delta is the pricing rule's δ (spec
// §8); Accounting, BrokerRelations, FactionStanding, and CorpStanding are
// the live skill/standing inputs Fees and OrderLimit were derived from
// (spec §4).
type RunParams struct {
	Budget          int64   `json:"budget"`
	TargetMargin    float64 `json:"target_margin"`
	Delta           float64 `json:"delta"`
	HorizonDays     int     `json:"horizon_days"`
	CaptureRate     float64 `json:"capture_rate"`
	Accounting      int     `json:"accounting"`
	BrokerRelations int     `json:"broker_relations"`
	FactionStanding float64 `json:"faction_standing"`
	CorpStanding    float64 `json:"corp_standing"`
}

// Summary is the three-way split of the candidate universe (spec §11):
// every candidate is funded, unfunded, or excluded. CommittedCapital and
// ExpectedDailyProfit total the funded set; BudgetUsed is CommittedCapital
// as a fraction of Budget; OrdersUsed is the funded set's active-order
// cost (spec §10: two slots per candidate).
type Summary struct {
	Recommendations     int     `json:"recommendations"`
	CommittedCapital    float64 `json:"committed_capital"`
	Budget              int64   `json:"budget"`
	BudgetUsed          float64 `json:"budget_used"`
	OrdersUsed          int     `json:"orders_used"`
	OrderLimit          int     `json:"order_limit"`
	ExpectedDailyProfit float64 `json:"expected_daily_profit"`
	Excluded            int     `json:"excluded"`
	Unfunded            int     `json:"unfunded"`
}

// Excluded records a candidate that failed a filter, and why (spec §7, §11).
type Excluded struct {
	TypeID int32  `json:"type_id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Result is the engine's entire public surface: the stable JSON contract
// every adapter renders (spec §11, §5).
type Result struct {
	Meta            Meta             `json:"meta"`
	Summary         Summary          `json:"summary"`
	BuyRecommendations []BuyRecommendation `json:"buy_recommendations"`
	Unfunded        []BuyRecommendation `json:"unfunded"`
	Excluded        []Excluded       `json:"excluded"`
}

// Params are the run's configurable inputs (spec §13); allocation
// thresholds are added by a later ticket.
type Params struct {
	RegionID       int32
	TradeStationID int64
	TradeSystemID  int32
	Delta          float64
	Fees           Fees

	// TargetMargin is the pricing rule's minimum net margin (spec §7 step
	// 8, §8): a candidate below it is excluded. Filters holds the
	// remaining book-only filter thresholds (spec §7 steps 5, 6; ticket
	// #19).
	TargetMargin float64
	Filters      FilterThresholds
}

// bestPrice returns the best bid (max price) if book is a buy book, or the
// best ask (min price) if book is a sell book, depending on which side the
// caller passes. Buy orders and sell orders are never mixed in one book, so
// the same "extreme price wins" reduction, oriented by whichever side it is
// given, serves both.
func bestPrice(book []Order) (float64, bool) {
	if len(book) == 0 {
		return 0, false
	}
	best := book[0].Price
	for _, o := range book[1:] {
		if o.IsBuyOrder {
			if o.Price > best {
				best = o.Price
			}
		} else {
			if o.Price < best {
				best = o.Price
			}
		}
	}
	return best, true
}
