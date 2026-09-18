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

// v1 filter thresholds (hardcoded constants — see docs/spec/v1.md §4). Not
// user-configurable in v1.
const (
	// MinMarginPct is the minimum gross margin percentage (M) an
	// opportunity must clear to be shown.
	MinMarginPct = 5.0

	// MinVolumePerDay is the minimum average daily volume (V_d) an
	// opportunity must clear to be shown.
	MinVolumePerDay = 10.0
)

// Opportunity is one ranked row: an item currently tradable at Rens, with
// its computed profitability figures under the trading character's
// current skills.
type Opportunity struct {
	TypeID        int
	Name          string
	Buy           float64 // P_b -- best (highest) current Rens buy order price
	Sell          float64 // P_s -- best (lowest) current Rens sell order price
	MarginPct     float64 // M   -- gross margin percentage
	ProfitPerUnit float64 // π   -- profit per unit after broker fee and sales tax
	VolumePerDay  float64 // V_d -- average daily Heimatar-region volume (approximation, see docs/spec/v1.md §3)
	ISKPerDay     float64 // EDP -- π × V_d, the primary rank
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

// compute returns the per-unit profit (π) and gross margin percentage (M)
// for a buy/sell price pair under the given skills.
func compute(buy, sell float64, skills Skills) (profitPerUnit, marginPct float64) {
	rb := BrokerFeeRate(skills.BrokerRelationsLevel)
	rt := SalesTaxRate(skills.AccountingLevel)

	profit := sell - buy - (buy * rb) - (sell * rb) - (sell * rt)
	margin := (sell - buy) / sell * 100

	return profit, margin
}

// opportunityQuery derives, per item_type currently present in
// market_order, the best (highest) current buy order price, the best
// (lowest) current sell order price, and the average daily volume over
// whatever market_history window is retained (the rolling window is
// maintained by the poller that writes market_history, not by this
// query). Items with no buy order or no sell order on the book are
// excluded -- there is no spread to compute.
const opportunityQuery = `
SELECT
	it.type_id,
	it.name,
	MAX(CASE WHEN mo.is_buy_order = 1 THEN mo.price END) AS buy_price,
	MIN(CASE WHEN mo.is_buy_order = 0 THEN mo.price END) AS sell_price,
	COALESCE((SELECT AVG(mh.volume) FROM market_history mh WHERE mh.type_id = it.type_id), 0) AS avg_volume
FROM item_type it
JOIN market_order mo ON mo.type_id = it.type_id
GROUP BY it.type_id, it.name
HAVING buy_price IS NOT NULL AND sell_price IS NOT NULL
`

// Load computes every opportunity that clears the v1 filter thresholds
// from the current database state, sorted by ISK/day descending (the
// default rank). Below-threshold items are excluded entirely, not just
// hidden.
func Load(ctx context.Context, db *sql.DB) ([]Opportunity, error) {
	skills, err := loadSkills(ctx, db)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, opportunityQuery)
	if err != nil {
		return nil, fmt.Errorf("querying opportunities: %w", err)
	}
	defer rows.Close()

	var out []Opportunity
	for rows.Next() {
		var o Opportunity
		if err := rows.Scan(&o.TypeID, &o.Name, &o.Buy, &o.Sell, &o.VolumePerDay); err != nil {
			return nil, fmt.Errorf("scanning opportunity row: %w", err)
		}

		o.ProfitPerUnit, o.MarginPct = compute(o.Buy, o.Sell, skills)
		o.ISKPerDay = o.ProfitPerUnit * o.VolumePerDay

		if o.MarginPct < MinMarginPct || o.VolumePerDay < MinVolumePerDay {
			continue
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading opportunity rows: %w", err)
	}

	Sort(out, "iskday")
	return out, nil
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
// "margin", "iskunit", "volday", or "iskday". Any other key (including
// "iskday" itself) falls back to the default rank: ISK/day descending.
func Sort(rows []Opportunity, by string) {
	switch by {
	case "buy":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Buy < rows[j].Buy })
	case "sell":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Sell < rows[j].Sell })
	case "margin":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].MarginPct > rows[j].MarginPct })
	case "iskunit":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ProfitPerUnit > rows[j].ProfitPerUnit })
	case "volday":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].VolumePerDay > rows[j].VolumePerDay })
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ISKPerDay > rows[j].ISKPerDay })
	}
}
