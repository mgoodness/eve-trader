package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// RenderTable renders a Result as the default dense table (spec §11): exact
// ISK, sorted by expected daily profit (NewResult's invariant — this
// function performs no sorting of its own), with the three
// always-distinguishable groups — funded recommendations, not funded by
// budget, and excluded by filters — accounting for every two-sided type. A
// zero-value Result (no candidates at all) renders cleanly: every section
// still prints its (0) count rather than panicking or omitting itself.
// explain expands the excluded section from a bare count to each item and
// its reason (spec §11 point 3, --explain).
func RenderTable(result engine.Result, explain bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "region %d \u00b7 trade station %d\n", result.Meta.RegionID, result.Meta.TradeStation)
	fmt.Fprintf(&b, "broker %s \u00b7 tax %s \u00b7 capture %s \u00b7 delta %s \u00b7 horizon %dd \u00b7 orders %d/%d\n\n",
		formatPercent(result.Meta.Fees.Broker, 2), formatPercent(result.Meta.Fees.SalesTax, 2),
		formatPercent(result.Meta.Params.CaptureRate, 0), formatISK(result.Meta.Params.Delta),
		result.Meta.Params.HorizonDays, result.Summary.OrdersUsed, result.Summary.OrderLimit)

	renderFunded(&b, result)
	renderUnfunded(&b, result)
	renderExcluded(&b, result, explain)

	return b.String()
}

func renderFunded(b *strings.Builder, result engine.Result) {
	fmt.Fprintf(b, "FUNDED RECOMMENDATIONS (%d)\n", len(result.Recommendations))
	if len(result.Recommendations) == 0 {
		b.WriteString("  none\n\n")
		return
	}

	fmt.Fprintf(b, "  %3s  %-30s%14s%14s%9s%9s%16s%15s\n",
		"#", "ITEM", "BUY", "SELL", "MARGIN", "UNITS", "COMMITTED", "EDP/DAY")
	for i, rec := range result.Recommendations {
		fmt.Fprintf(b, "  %3d  %-30s%14s%14s%9s%9s%16s%15s\n",
			i+1, displayName(rec.TypeID, rec.Name), formatISK(rec.BuyPrice), formatISK(rec.SellPrice),
			formatPercent(rec.NetMargin, 1), formatISK(float64(rec.Units)),
			formatISK(rec.CommittedCapital), formatISK(rec.ExpectedDailyProfit))
	}
	fmt.Fprintf(b, "\n  %d flip(s) \u00b7 committed %s / %s ISK (%s of budget) \u00b7 total EDP/day %s\n\n",
		len(result.Recommendations), formatISK(result.Summary.CommittedCapital), formatISK(float64(result.Summary.Budget)),
		formatPercent(result.Summary.BudgetUsed, 1), formatISK(result.Summary.ExpectedDailyProfit))
}

func renderUnfunded(b *strings.Builder, result engine.Result) {
	fmt.Fprintf(b, "NOT FUNDED BY BUDGET (%d)\n", len(result.Unfunded))
	if len(result.Unfunded) == 0 {
		b.WriteString("  none\n\n")
		return
	}

	for _, rec := range result.Unfunded {
		needs := engine.CapitalNeeded(rec, result.Meta.Fees.Broker, result.Meta.Params.CaptureRate, result.Meta.Params.HorizonDays)
		fmt.Fprintf(b, "  %-30s%9s   EDP/day %15s   needs %s ISK\n",
			displayName(rec.TypeID, rec.Name), formatPercent(rec.NetMargin, 1), formatISK(rec.ExpectedDailyProfit), formatISK(needs))
	}
	b.WriteString("\n")
}

func renderExcluded(b *strings.Builder, result engine.Result, explain bool) {
	if !explain {
		fmt.Fprintf(b, "EXCLUDED BY FILTERS (%d) \u2014 pass --explain for the item list and reasons\n", len(result.Excluded))
		return
	}

	fmt.Fprintf(b, "EXCLUDED BY FILTERS (%d)\n", len(result.Excluded))
	if len(result.Excluded) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, e := range result.Excluded {
		fmt.Fprintf(b, "  %-34s %s\n", displayName(e.TypeID, e.Name), e.Reason)
	}
}

// displayName is name if it is known, or a "type <id>" placeholder
// otherwise. The whole-universe pipeline resolves names through the batched,
// disk-cached ESI lookup (internal/cli/names.go); a row must still identify
// its item when that lookup genuinely cannot resolve an id, rather than
// rendering a blank column.
func displayName(typeID int32, name string) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("type %d", typeID)
}

// formatISK renders v as a whole, comma-grouped ISK amount (spec §11:
// "exact ISK" — no compaction to k/M/B).
func formatISK(v float64) string {
	n := int64(v + 0.5)
	if v < 0 {
		n = -int64(-v + 0.5)
	}
	return groupThousands(strconv.FormatInt(n, 10))
}

func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	n := len(s)
	if n <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out strings.Builder
	lead := n % 3
	if lead > 0 {
		out.WriteString(s[:lead])
	}
	for i := lead; i < n; i += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(s[i : i+3])
	}
	result := out.String()
	if neg {
		return "-" + result
	}
	return result
}

// formatPercent renders a fraction (e.g. 0.018) as a percentage with decimals
// digits of precision (e.g. "1.80%").
func formatPercent(v float64, decimals int) string {
	return strconv.FormatFloat(v*100, 'f', decimals, 64) + "%"
}
