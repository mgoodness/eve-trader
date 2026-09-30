package engine

import "time"

// Fees are the pilot's fee rates, read live from ESI (skills + standings) in
// later tickets; a fixed rate is passed in for the walking skeleton (ticket
// #14). Broker is charged on both legs of a round trip; SalesTax on the sell
// leg only (spec §4).
type Fees struct {
	Broker   float64 `json:"broker"`
	SalesTax float64 `json:"sales_tax"`
}

// Meta describes the run that produced a Result (spec §11).
type Meta struct {
	GeneratedAt  time.Time `json:"generated_at"`
	RegionID     int32     `json:"region_id"`
	TradeStation int64     `json:"trade_station"`
	Fees         Fees      `json:"fees"`
}

// Summary is the three-way split of the candidate universe (spec §11):
// every candidate is funded, unfunded, or excluded.
type Summary struct {
	Recommendations int `json:"recommendations"`
	Excluded        int `json:"excluded"`
	Unfunded        int `json:"unfunded"`
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
	Recommendations []Recommendation `json:"recommendations"`
	Unfunded        []Recommendation `json:"unfunded"`
	Excluded        []Excluded       `json:"excluded"`
}

// Params are the run's configurable inputs (spec §13); allocation and filter
// thresholds are added by later tickets.
type Params struct {
	RegionID       int32
	TradeStationID int64
	TradeSystemID  int32
	Delta          float64
	Fees           Fees
}

// Recommend builds the Result for a single candidate type: it derives the
// effective books, prices the front of queue, and classifies the candidate
// as recommended or excluded. This is the walking skeleton's entire engine
// surface (ticket #14); later tickets extend it to the full filter/rank/
// allocate pipeline over the whole candidate universe. jumpDistances is
// forwarded to EffectiveBuyBook unchanged for numeric-range coverage
// (ticket #18); pass nil if the caller has none.
func Recommend(orders []Order, typeID int32, name string, params Params, jumpDistances map[int32]int, generatedAt time.Time) Result {
	result := Result{
		Meta: Meta{
			GeneratedAt:  generatedAt,
			RegionID:     params.RegionID,
			TradeStation: params.TradeStationID,
			Fees:         params.Fees,
		},
		Recommendations: []Recommendation{},
		Unfunded:        []Recommendation{},
		Excluded:        []Excluded{},
	}

	sellBook := EffectiveSellBook(orders, params.TradeStationID)
	buyBook := EffectiveBuyBook(orders, params.TradeStationID, params.TradeSystemID, jumpDistances)

	bestBid, haveBid := bestPrice(buyBook)
	bestAsk, haveAsk := bestPrice(sellBook)

	if !haveBid || !haveAsk {
		result.Excluded = append(result.Excluded, Excluded{
			TypeID: typeID,
			Name:   name,
			Reason: "no two-sided book: missing a covering best bid or a station best ask",
		})
		result.Summary.Excluded = 1
		return result
	}

	rec, ok := Price(typeID, name, bestBid, bestAsk, params.Delta, params.Fees.Broker, params.Fees.SalesTax)
	if !ok {
		result.Excluded = append(result.Excluded, Excluded{
			TypeID: typeID,
			Name:   name,
			Reason: "crossed book: 2\u03b4 \u2265 spread",
		})
		result.Summary.Excluded = 1
		return result
	}

	result.Recommendations = append(result.Recommendations, rec)
	result.Summary.Recommendations = 1
	return result
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
