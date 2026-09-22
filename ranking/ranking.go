// Package ranking computes eve-trader's opportunity list: the v1
// profitability/ranking formula (see docs/spec/v1.md §4) applied to the
// whole Heimatar region order book (see
// docs/adr/0005-heimatar-region-as-pricing-market.md), item names, and the
// trading character's fee-relevant skills, all read directly from SQLite
// (market_order, market_history, item_type, character_skill).
package ranking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/fees"
	"github.com/mgoodness/eve-trader/internal/skills"
	"github.com/mgoodness/eve-trader/internal/standings"
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

// Opportunity is one ranked row: an item with a region best buy and a
// region best sell, with its computed profitability figures under the
// trading character's current skills. The row is a region-wide pricing
// signal; it does not require an order at Rens.
type Opportunity struct {
	TypeID         int
	Name           string
	Buy            float64 // P_b -- Region best buy: highest buy order price anywhere in Heimatar
	Sell           float64 // P_s -- Region best sell: lowest sell order price anywhere in Heimatar
	GrossMarginPct float64 // M   -- gross margin percentage, before fees (drives the minimum-margin filter)
	NetMarginPct   float64 // net margin percentage -- profit after fees as a fraction of sell price (displayed)
	ProfitPerUnit  float64 // π   -- profit per unit after broker fee and sales tax
	VolumePerDay   float64 // V_d -- average daily Heimatar-region volume (approximation, see docs/spec/v1.md §3)
	ISKPerDay      float64 // EDP -- π × V_d × CaptureRate, the primary rank
}

// Skills holds the fee/tax-relevant skill levels used in the ranking
// formula. It is the shared skills.Skills type so ranking and the portfolio
// loader agree on how a missing row resolves.
type Skills = skills.Skills

// FeeRates bundles the broker-fee rate R_b and sales-tax rate R_t that
// apply to the tracked character at Rens, after both skills and standings
// (docs/spec/v2.md §4.3, §9). LoadFeeRates is the one place they are
// derived, so the opportunity ranking, the portfolio, and the derived
// minimum-margin default never drift.
type FeeRates struct {
	// Broker is R_b, charged on both the buy and sell side.
	Broker float64
	// Tax is R_t, charged on the sell side.
	Tax float64
}

// LoadFeeRates loads the character's skills and the Rens station owner's
// (corp and faction) standings and computes both rates once. A missing
// skill row or an unresolved station owner resolves to the level-0,
// no-standings baseline rather than an error.
func LoadFeeRates(ctx context.Context, db *sql.DB) (FeeRates, error) {
	sk, err := LoadSkills(ctx, db)
	if err != nil {
		return FeeRates{}, err
	}
	characterID, err := trackedCharacterID(ctx, db)
	if err != nil {
		return FeeRates{}, err
	}
	st, err := standings.Load(ctx, db, characterID, esi.RensStationID)
	if err != nil {
		return FeeRates{}, err
	}
	return FeeRates{
		Broker: fees.BrokerFeeRate(sk.BrokerRelationsLevel, st.Corp, st.Faction),
		Tax:    fees.SalesTaxRate(sk.AccountingLevel),
	}, nil
}

// trackedCharacterID returns the single stored character id, or 0 when no
// token has been stored yet (the first-boot state). It mirrors how the
// skills and standings loaders pick their single row, so all three resolve
// the same character.
func trackedCharacterID(ctx context.Context, db *sql.DB) (int, error) {
	var id int
	err := db.QueryRowContext(ctx, `SELECT character_id FROM esi_token LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("loading tracked character id: %w", err)
	}
	return id, nil
}

// BreakEvenGrossMargin is the gross margin percentage at which a trade
// exactly clears the broker fee charged on both sides and the sales tax on
// the sell side, for the given rates:
//
//	g* = (2·R_b + R_t) / (1 + R_b)
//
// It is rounded up to one decimal place, so a default set to it never dips
// below break-even. A missing character_skill or station-owner row has
// already resolved to the no-standings, level-0 baseline in LoadFeeRates,
// so no separate fallback is needed here.
func BreakEvenGrossMargin(rates FeeRates) float64 {
	return math.Ceil((2*rates.Broker+rates.Tax)/(1+rates.Broker)*100*10) / 10
}

// compute returns the per-unit profit (π), the gross margin percentage (M),
// and the net margin percentage (π as a fraction of sell price) for a
// buy/sell price pair under the given fee rates.
func compute(buy, sell float64, rates FeeRates) (profitPerUnit, grossMarginPct, netMarginPct float64) {
	profit := sell - buy - (buy * rates.Broker) - (sell * rates.Broker) - (sell * rates.Tax)
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
// Region best buy and a Region best sell, whether shown or hidden.
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

// opportunityQuery derives, per candidate item, its Region best buy
// (highest) and Region best sell (lowest) price across the whole region
// book, its retained-window volume and realism aggregates (trade-day
// count, incomplete-price count, high/low swing), and the number of orders
// within the near-best band on each side. Items with no region buy order
// or no region sell order are excluded -- there is no spread to compute.
// The window itself is maintained by the poller that writes
// market_history, not by this query.
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
	rates, err := LoadFeeRates(ctx, db)
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

		o.ProfitPerUnit, o.GrossMarginPct, o.NetMarginPct = compute(o.Buy, o.Sell, rates)
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

// LoadSkills reads the single character_skill row. A missing row (no
// character seeded yet) resolves to level-0 skills rather than an error,
// mirroring ESIGateway's "missing skill ID = level 0" convention. Callers
// that need the fee-relevant skills outside a ranking pass -- e.g. the
// skills-derived minimum-margin default -- use it directly.
func LoadSkills(ctx context.Context, db *sql.DB) (Skills, error) {
	return skills.Load(ctx, db)
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
