package cli

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// RenderTable renders a Result as the default dense table (spec §14): the
// run's provenance, a summary splitting the budget and the order limit each
// into reserved-by-existing-orders / available / used-this-run, the buy
// side's three always-distinguishable groups — funded recommendations
// (sorted by expected daily profit, NewResult's invariant), not funded by
// budget, and excluded by filters — then a distinct sell table (sorted
// worst margin shortfall first, RecommendSells' invariant), and the pending
// bucket as a count by reason. This function performs no sorting of its
// own: both orders are the data's invariants, established upstream. A
// zero-value Result (no candidates at all) renders cleanly: every section
// still prints its (0) count rather than panicking or omitting itself.
// explain expands the excluded and pending sections from bare counts to
// each item and its reason (spec §11 point 3, --explain). The two tables
// size their columns from the actual row data, never a hardcoded guess (a
// cell such as the sell table's "X% on Y/Zu" partial-pricing notation can
// outrun its header).
func RenderTable(result engine.Result, explain bool) string {
	var b strings.Builder

	renderHeader(&b, result)
	renderSummary(&b, result)
	renderFunded(&b, result)
	renderUnfunded(&b, result)
	renderExcluded(&b, result, explain)
	renderSells(&b, result)
	renderPending(&b, result, explain)

	return b.String()
}

// renderHeader prints the run's provenance line: the region and trade
// station the prices were read from, and the live fees and run params they
// were computed at (spec §14).
func renderHeader(b *strings.Builder, result engine.Result) {
	fmt.Fprintf(b, "region %d \u00b7 trade station %d\n", result.Meta.RegionID, result.Meta.TradeStation)
	fmt.Fprintf(b, "broker %s \u00b7 tax %s \u00b7 capture %s \u00b7 delta %s \u00b7 horizon %dd\n\n",
		formatPercent(result.Meta.Fees.Broker, 2), formatPercent(result.Meta.Fees.SalesTax, 2),
		formatPercent(result.Meta.Params.CaptureRate, 0), formatISK(result.Meta.Params.Delta),
		result.Meta.Params.HorizonDays)
}

// renderSummary prints the run's resource split (spec §13, §14): the stated
// budget and the skill-derived order limit each divided into what
// pre-existing open orders already reserved, what was therefore available
// to allocate, and what this run actually used. Budget "used this run" is
// the funded buys' committed capital (sells cost no budget); order "used"
// counts every funded recommendation, buy or sell, at one slot each
// (decision 4).
func renderSummary(b *strings.Builder, result engine.Result) {
	s := result.Summary
	b.WriteString("SUMMARY\n")
	fmt.Fprintf(b, "  budget       %s ISK  (reserved by existing orders: %s; available: %s; used this run: %s)\n",
		formatISK(float64(s.Budget)), formatISK(s.BudgetReservedByExisting),
		formatISK(s.BudgetAvailable), formatISK(s.CommittedCapital))
	fmt.Fprintf(b, "  order limit  %d  (reserved by existing orders: %d; available: %d; used this run: %d)\n\n",
		s.OrderLimit, s.OrdersReservedByExisting, s.OrdersAvailable, s.OrdersUsed)
}

func renderFunded(b *strings.Builder, result engine.Result) {
	fmt.Fprintf(b, "FUNDED RECOMMENDATIONS (%d)\n", len(result.BuyRecommendations))
	if len(result.BuyRecommendations) == 0 {
		b.WriteString("  none\n\n")
		return
	}

	header := []string{"#", "ITEM", "BUY", "SELL", "MARGIN", "UNITS", "COMMITTED", "EDP/DAY"}
	aligns := []columnAlignment{alignRight, alignLeft, alignRight, alignRight, alignRight, alignRight, alignRight, alignRight}
	rows := make([][]string, 0, len(result.BuyRecommendations))
	for i, rec := range result.BuyRecommendations {
		rows = append(rows, []string{
			strconv.Itoa(i + 1),
			displayName(rec.TypeID, rec.Name),
			formatISK(rec.BuyPrice),
			formatISK(rec.SellPrice),
			formatPercent(rec.NetMargin, 1),
			formatISK(float64(rec.Units)),
			formatISK(rec.CommittedCapital),
			formatISK(rec.ExpectedDailyProfit),
		})
	}
	renderTable(b, header, aligns, rows)
	fmt.Fprintf(b, "\n  %d flip(s) \u00b7 committed %s / %s ISK (%s of budget) \u00b7 total EDP/day %s\n\n",
		len(result.BuyRecommendations), formatISK(result.Summary.CommittedCapital), formatISK(float64(result.Summary.Budget)),
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
		fmt.Fprintf(b, "EXCLUDED BY FILTERS (%d) \u2014 pass --explain for the item list and reasons\n\n", len(result.Excluded))
		return
	}

	fmt.Fprintf(b, "EXCLUDED BY FILTERS (%d)\n", len(result.Excluded))
	if len(result.Excluded) == 0 {
		b.WriteString("  none\n\n")
		return
	}
	for _, e := range result.Excluded {
		fmt.Fprintf(b, "  %-34s %s\n", displayName(e.TypeID, e.Name), e.Reason)
	}
	b.WriteString("\n")
}

// pendingReasons is the render order for the pending summary (spec §11).
var pendingReasons = []string{
	engine.PendingAwaitingBuyFill,
	engine.PendingAwaitingSellFill,
	engine.PendingUnknownOutcome,
	engine.PendingOrderLimit,
}

// columnAlignment is a table column's horizontal alignment: left for text,
// right for numbers.
type columnAlignment int

const (
	alignLeft columnAlignment = iota
	alignRight
)

// renderTable writes header and rows as a dense table whose column widths
// are the maximum of the header label and every cell in that column (spec
// §14: widths must come from the actual row data, never a hardcoded guess —
// a cell such as the sell table's "X% on Y/Zu" partial-pricing notation can
// run longer than its header). Columns are separated by two spaces; each
// line's trailing spaces are trimmed, so a table whose last column is
// right-aligned has header and rows of equal length.
func renderTable(b *strings.Builder, header []string, aligns []columnAlignment, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}

	writeLine := func(cells []string) {
		var line strings.Builder
		for i := range header {
			if i > 0 {
				line.WriteString("  ")
			}
			var cell string
			if i < len(cells) {
				cell = cells[i]
			}
			pad := widths[i] - displayWidth(cell)
			if pad < 0 {
				pad = 0
			}
			if aligns[i] == alignRight {
				line.WriteString(strings.Repeat(" ", pad))
				line.WriteString(cell)
			} else {
				line.WriteString(cell)
				line.WriteString(strings.Repeat(" ", pad))
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}

	writeLine(header)
	for _, row := range rows {
		writeLine(row)
	}
}

// displayWidth is the number of runes in s, the unit fmt pads by; a byte
// count would over-pad any non-ASCII item name.
func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}

// renderSells prints the sell recommendations (spec §11, §14): one distinct
// table sorted worst margin shortfall first, with item, the full held
// quantity, the front-of-queue sell price, the §12 net margin (annotated
// "X% on Y/Zu" when only part of the quantity is priced), a below-target
// flag, net proceeds, and the ledger lot count. The renderer does not sort:
// RecommendSells' worst-shortfall-first order is the data's invariant (spec
// §11), just as NewResult's expected-daily-profit order is for the buy side.
// A zero-value Result prints a (0) count cleanly.
func renderSells(b *strings.Builder, result engine.Result) {
	fmt.Fprintf(b, "SELL RECOMMENDATIONS (%d) \u2014 sorted by worst margin shortfall first\n", len(result.SellRecommendations))
	if len(result.SellRecommendations) == 0 {
		b.WriteString("  none\n\n")
		return
	}

	header := []string{"item", "units", "sell price", "net margin", "flag", "net proceeds", "lots"}
	aligns := []columnAlignment{alignLeft, alignRight, alignRight, alignLeft, alignLeft, alignRight, alignRight}
	rows := make([][]string, 0, len(result.SellRecommendations))
	for _, rec := range result.SellRecommendations {
		margin := formatPercent(rec.NetMargin, 1)
		if rec.UnpricedQuantity > 0 {
			// "X% on Y/Zu": only Y of Z units have a known acquisition price
			// (the rest come from seeded lots, ADR 0004).
			margin = fmt.Sprintf("%s on %d/%du", margin, rec.PricedQuantity, rec.Quantity)
		}
		flag := ""
		if rec.BelowTarget {
			flag = "below target"
		}
		rows = append(rows, []string{
			displayName(rec.TypeID, rec.Name),
			formatISK(float64(rec.Quantity)),
			formatISK(rec.SellPrice),
			margin,
			flag,
			formatISK(rec.NetProceeds),
			strconv.Itoa(len(rec.Lots)),
		})
	}
	renderTable(b, header, aligns, rows)
	b.WriteString("\n  units: full held-unlisted quantity. \"X% on Y/Zu\": only Y of Z units have a known acquisition price, the rest are seeded (ADR 0004). flag: below your target margin \u2014 still recommended, just called out (ADR 0005). net proceeds: gross revenue less sales tax and this order's own broker fee, not netted against acquisition cost.\n\n")
}

// renderPending prints the pending bucket (spec §11, §14): a count by reason
// by default, expanded to each entry's item, reason, and detail under
// --explain (decision 10 — the same convention as the excluded bucket).
func renderPending(b *strings.Builder, result engine.Result, explain bool) {
	if !explain {
		fmt.Fprintf(b, "PENDING (%d)", len(result.Pending))
		for _, reason := range pendingReasons {
			count := 0
			for _, p := range result.Pending {
				if p.Reason == reason {
					count++
				}
			}
			if count > 0 {
				fmt.Fprintf(b, " \u00b7 %s %d", reason, count)
			}
		}
		b.WriteString(" — pass --explain for the item list and reasons\n")
		return
	}

	fmt.Fprintf(b, "PENDING (%d)\n", len(result.Pending))
	if len(result.Pending) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, p := range result.Pending {
		fmt.Fprintf(b, "  %-34s %-18s %s\n", displayName(p.TypeID, p.Name), p.Reason, p.Detail)
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
