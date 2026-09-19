// Package ranking computes eve-trader's opportunity list: the v1
// profitability/ranking formula (see docs/spec/v1.md §4) applied to Rens's
// current order book, item names, and the trading character's fee-relevant
// skills, all read directly from SQLite (market_order, market_history,
// item_type, character_skill).
package ranking

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// CaptureRate is the fixed fraction of an item's average daily
// Heimatar-region volume a single trader is assumed to capture when
// computing ISK/day. It is a deliberate v1.1 assumption (EVE station
// trading cannot capture the whole market) and deliberately not
// user-configurable; per-unit profit and the displayed Vol/day stay raw.
const CaptureRate = 0.20

// Always-on realism filter thresholds. Unlike the user-adjustable filters
// in Filters these are not adjustable: an item that fails any of them is
// excluded from the list entirely, silently and with no reveal toggle.
const (
	// MinTradeDays is the fewest recent trade-days (history days with
	// order_count > 0) an item must have over the retained window.
	MinTradeDays = 7

	// ManipulatedTradeDays is the trade-day count below which a wide
	// high/low swing marks an item's history as manipulated.
	ManipulatedTradeDays = 14

	// ManipulatedSwingRatio is the max(highest)/min(lowest) ratio above
	// which a sub-ManipulatedTradeDays window is treated as manipulated.
	ManipulatedSwingRatio = 20.0

	// NearBookBand is how close to the best price an order must sit to
	// count toward the "real" side of a spread. A side with one or fewer
	// orders inside the band is a single-order spread.
	NearBookBand = 0.05
)

// Opportunity is one ranked row: an item currently tradable at Rens, with
// its computed profitability figures under the trading character's
// current skills.
type Opportunity struct {
	TypeID         int
	Name           string
	Buy            float64 // P_b -- best (highest) current Rens buy order price
	Sell           float64 // P_s -- best (lowest) current Rens sell order price
	GrossMarginPct float64 // M   -- gross margin percentage, before fees (drives the v1 filter)
	NetMarginPct   float64 // net margin percentage -- profit after fees as a fraction of sell price (displayed)
	ProfitPerUnit  float64 // π   -- profit per unit after broker fee and sales tax
	VolumePerDay   float64 // V_d -- average daily Heimatar-region volume (approximation, see docs/spec/v1.md §3)
	ISKPerDay      float64 // EDP -- π × V_d × CaptureRate, the primary rank
}

// Skills holds the fee/tax-relevant skill levels used in the ranking
// formula: Broker Relations and Accounting active_skill_level.
type Skills struct {
	BrokerRelationsLevel int
	AccountingLevel      int
}

// BrokerFeeRate is R_b, the broker fee rate charged on both the buy and
// sell side, for a given Broker Relations skill level. The standings term
// is deliberately not modeled in v1 (see docs/spec/v1.md §4).
func BrokerFeeRate(brokerRelationsLevel int) float64 {
	return 0.03 - 0.003*float64(brokerRelationsLevel)
}

// SalesTaxRate is R_t, the sales tax rate charged on the sell side, for a
// given Accounting skill level.
func SalesTaxRate(accountingLevel int) float64 {
	return 0.075 * (1 - 0.11*float64(accountingLevel))
}

// compute returns the per-unit profit (π), the gross margin percentage (M),
// and the net margin percentage (π as a fraction of sell price) for a
// buy/sell price pair under the given skills.
func compute(buy, sell float64, skills Skills) (profitPerUnit, grossMarginPct, netMarginPct float64) {
	rb := BrokerFeeRate(skills.BrokerRelationsLevel)
	rt := SalesTaxRate(skills.AccountingLevel)

	profit := sell - buy - (buy * rb) - (sell * rb) - (sell * rt)
	grossMargin := (sell - buy) / sell * 100
	netMargin := profit / sell * 100

	return profit, grossMargin, netMargin
}

// Filters are the user-adjustable bounds from the v1.1 filter form. Each
// field is optional: a nil pointer means the control was submitted blank,
// which removes that bound. Bounds are inclusive, and a filter only ever
// excludes a row -- it never rewrites a price. An absent control is filled
// in with its default by the URL-parsing layer, so ranking always receives
// concrete bounds here.
type Filters struct {
	MinVolume *float64 // minimum average daily volume (V_d), inclusive
	MinMargin *float64 // minimum gross margin percentage, inclusive
	MaxMargin *float64 // maximum gross margin percentage, inclusive
	MaxSell   *float64 // maximum sell price, inclusive
}

// Passes reports whether o clears every active user filter. A nil bound is
// no bound.
func (f Filters) Passes(o Opportunity) bool {
	if f.MinVolume != nil && o.VolumePerDay < *f.MinVolume {
		return false
	}
	if f.MinMargin != nil && o.GrossMarginPct < *f.MinMargin {
		return false
	}
	if f.MaxMargin != nil && o.GrossMarginPct > *f.MaxMargin {
		return false
	}
	if f.MaxSell != nil && o.Sell > *f.MaxSell {
		return false
	}
	return true
}

// Result is one ranking pass: the opportunities that clear both the
// always-on realism filters and the user filters, plus how many candidate
// items (an item with a buy and a sell side on the book) each tier hid.
// The hidden counts let the page make the exclusions visible without
// revealing the excluded rows.
type Result struct {
	Opportunities   []Opportunity
	HiddenByRealism int
	HiddenByFilters int
}

// Total is the candidate set the filters ran over: every item with both a
// buy and a sell side on the Rens book, whether shown or hidden.
func (r Result) Total() int {
	return len(r.Opportunities) + r.HiddenByRealism + r.HiddenByFilters
}

// historyStats is the realism evidence gathered for one candidate item:
// aggregates over its retained market_history window and per-side order
// counts near the current best prices.
type historyStats struct {
	historyDays  int
	tradeDays    int
	unpricedDays int
	maxHigh      sql.NullFloat64
	minLow       sql.NullFloat64
	buyNear      int
	sellNear     int
}

// realismPasses reports whether the item clears every always-on realism
// filter. An incomplete history window is rejected before its aggregates
// are trusted; thin history and single-order spreads are rejected outright;
// a wide swing only condemns an already-thin window.
func (h historyStats) realismPasses() bool {
	if h.historyDays == 0 || h.unpricedDays > 0 {
		return false // incomplete history (not yet re-fetched after migration)
	}
	if h.tradeDays < MinTradeDays {
		return false // thin history
	}
	if h.tradeDays < ManipulatedTradeDays && h.swing() > ManipulatedSwingRatio {
		return false // manipulated history
	}
	if h.buyNear <= 1 || h.sellNear <= 1 {
		return false // single-order spread
	}
	return true
}

// swing is max(highest)/min(lowest) over the retained window, or 0 when
// the window carries no usable low price.
func (h historyStats) swing() float64 {
	if !h.maxHigh.Valid || !h.minLow.Valid || h.minLow.Float64 <= 0 {
		return 0
	}
	return h.maxHigh.Float64 / h.minLow.Float64
}

// opportunityQuery derives, per candidate item, its best (highest) buy and
// best (lowest) sell price, its retained-window volume and realism
// aggregates (trade-day count, incomplete-price count, high/low swing),
// and the number of orders within the near-best band on each side. Items
// with no buy order or no sell order on the book are excluded -- there is
// no spread to compute. The window itself is maintained by the poller that
// writes market_history, not by this query.
const opportunityQuery = `
WITH book AS (
	SELECT
		type_id,
		MAX(CASE WHEN is_buy_order = 1 THEN price END) AS buy_price,
		MIN(CASE WHEN is_buy_order = 0 THEN price END) AS sell_price
	FROM market_order
	GROUP BY type_id
),
hist AS (
	SELECT
		type_id,
		AVG(volume) AS avg_volume,
		COUNT(*) AS history_days,
		SUM(CASE WHEN order_count > 0 THEN 1 ELSE 0 END) AS trade_days,
		SUM(CASE WHEN average IS NULL OR highest IS NULL OR lowest IS NULL THEN 1 ELSE 0 END) AS unpriced_days,
		MAX(highest) AS max_high,
		MIN(lowest) AS min_low
	FROM market_history
	GROUP BY type_id
)
SELECT
	it.type_id,
	it.name,
	b.buy_price,
	b.sell_price,
	COALESCE(h.avg_volume, 0) AS avg_volume,
	COALESCE(h.history_days, 0) AS history_days,
	COALESCE(h.trade_days, 0) AS trade_days,
	COALESCE(h.unpriced_days, 0) AS unpriced_days,
	h.max_high,
	h.min_low,
	(SELECT COUNT(*) FROM market_order o WHERE o.type_id = it.type_id AND o.is_buy_order = 1 AND o.price >= ? * b.buy_price) AS buy_near,
	(SELECT COUNT(*) FROM market_order o WHERE o.type_id = it.type_id AND o.is_buy_order = 0 AND o.price <= ? * b.sell_price) AS sell_near
FROM item_type it
JOIN book b ON b.type_id = it.type_id
LEFT JOIN hist h ON h.type_id = it.type_id
WHERE b.buy_price IS NOT NULL AND b.sell_price IS NOT NULL
`

// Load computes every opportunity that clears the always-on realism
// filters and then the caller's user filters from the current database
// state, sorted by ISK/day descending (the default rank). Excluded items
// are dropped entirely, not just hidden, and the tier that excluded each is
// counted so the page can report both.
func Load(ctx context.Context, db *sql.DB, filters Filters) (Result, error) {
	skills, err := loadSkills(ctx, db)
	if err != nil {
		return Result{}, err
	}

	rows, err := db.QueryContext(ctx, opportunityQuery, 1-NearBookBand, 1+NearBookBand)
	if err != nil {
		return Result{}, fmt.Errorf("querying opportunities: %w", err)
	}
	defer rows.Close()

	var result Result
	for rows.Next() {
		var (
			o Opportunity
			h historyStats
		)
		if err := rows.Scan(
			&o.TypeID, &o.Name, &o.Buy, &o.Sell, &o.VolumePerDay,
			&h.historyDays, &h.tradeDays, &h.unpricedDays,
			&h.maxHigh, &h.minLow, &h.buyNear, &h.sellNear,
		); err != nil {
			return Result{}, fmt.Errorf("scanning opportunity row: %w", err)
		}

		if !h.realismPasses() {
			result.HiddenByRealism++
			continue
		}

		o.ProfitPerUnit, o.GrossMarginPct, o.NetMarginPct = compute(o.Buy, o.Sell, skills)
		o.ISKPerDay = o.ProfitPerUnit * o.VolumePerDay * CaptureRate

		if !filters.Passes(o) {
			result.HiddenByFilters++
			continue
		}
		result.Opportunities = append(result.Opportunities, o)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("reading opportunity rows: %w", err)
	}

	Sort(result.Opportunities, "iskday")
	return result, nil
}

// loadSkills reads the single character_skill row. A missing row (no
// character seeded yet) resolves to level-0 skills rather than an error,
// mirroring ESIGateway's "missing skill ID = level 0" convention.
func loadSkills(ctx context.Context, db *sql.DB) (Skills, error) {
	var s Skills
	err := db.QueryRowContext(ctx,
		`SELECT broker_relations_level, accounting_level FROM character_skill ORDER BY character_id LIMIT 1`,
	).Scan(&s.BrokerRelationsLevel, &s.AccountingLevel)
	if err == sql.ErrNoRows {
		return Skills{}, nil
	}
	if err != nil {
		return Skills{}, fmt.Errorf("loading character skills: %w", err)
	}
	return s, nil
}

// Sort reorders rows in place by the given column key: "buy", "sell",
// "margin", "iskunit", "volday", or "iskday". The "margin" key sorts by
// net margin (the figure the column displays). Any other key (including
// "iskday" itself) falls back to the default rank: ISK/day descending.
func Sort(rows []Opportunity, by string) {
	switch by {
	case "buy":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Buy < rows[j].Buy })
	case "sell":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Sell < rows[j].Sell })
	case "margin":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].NetMarginPct > rows[j].NetMarginPct })
	case "iskunit":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ProfitPerUnit > rows[j].ProfitPerUnit })
	case "volday":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].VolumePerDay > rows[j].VolumePerDay })
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ISKPerDay > rows[j].ISKPerDay })
	}
}
