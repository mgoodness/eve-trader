package cli_test

import (
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

func TestRenderTableListsFundedBuyRecommendationsInExactISKSortedByExpectedDailyProfit(t *testing.T) {
	result := engine.Result{
		BuyRecommendations: []engine.BuyRecommendation{
			{Name: "1600mm Steel Plates II", BuyPrice: 1720100, SellPrice: 3697900, NetMargin: 0.4791, Units: 17, CommittedCapital: 30623290, ExpectedDailyProfit: 10039780},
			{Name: "Gyrostabilizer II", BuyPrice: 700200, SellPrice: 927200, NetMargin: 0.1847, Units: 110, CommittedCapital: 79707210, ExpectedDailyProfit: 6280853},
		},
	}

	out := cli.RenderTable(result, false)

	plateIdx := strings.Index(out, "1600mm Steel Plates II")
	gyroIdx := strings.Index(out, "Gyrostabilizer II")
	if plateIdx == -1 || gyroIdx == -1 {
		t.Fatalf("got table %q, want both item names present", out)
	}
	if plateIdx > gyroIdx {
		t.Errorf("got 1600mm Steel Plates II after Gyrostabilizer II, want it first (higher expected daily profit)")
	}
	if !strings.Contains(out, "1,720,100") {
		t.Errorf("got table %q, want exact ISK buy price 1,720,100", out)
	}
	if !strings.Contains(out, "3,697,900") {
		t.Errorf("got table %q, want exact ISK sell price 3,697,900", out)
	}
}

func TestRenderTableShowsNotFundedByBudgetSectionWithMarginEDPAndCapitalNeeded(t *testing.T) {
	result := engine.Result{
		Meta: engine.Meta{
			Fees:   engine.Fees{Broker: 0.018},
			Params: engine.RunParams{CaptureRate: 0.20, HorizonDays: 3},
		},
		Unfunded: []engine.BuyRecommendation{
			{Name: "Multispectrum Shield Hardener II", BuyPrice: 1415100, SellPrice: 1994900, NetMargin: 0.2313, ExpectedDailyProfit: 11379279, AverageDailyVolume: 124.9},
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "NOT FUNDED") {
		t.Fatalf("got table %q, want a \"not funded by budget\" section", out)
	}
	if !strings.Contains(out, "Multispectrum Shield Hardener II") {
		t.Errorf("got table %q, want the unfunded item's name", out)
	}
	if !strings.Contains(out, "23.1%") {
		t.Errorf("got table %q, want the unfunded item's net margin", out)
	}
	if !strings.Contains(out, "11,379,279") {
		t.Errorf("got table %q, want the unfunded item's expected daily profit", out)
	}
	// capital needed = (1415100*1.018 + 1994900*0.018) * (0.20*124.9*3 = 74) units.
	if !strings.Contains(out, "needs") {
		t.Errorf("got table %q, want a \"needs ... ISK\" capital figure", out)
	}
}

func TestRenderTableShowsExcludedCountWithoutReasonsByDefault(t *testing.T) {
	result := engine.Result{
		Excluded: []engine.Excluded{
			{TypeID: 34, Name: "Tritanium", Reason: "thin book"},
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "1") {
		t.Errorf("got table %q, want the excluded count (1)", out)
	}
	if strings.Contains(out, "Tritanium") || strings.Contains(out, "thin book") {
		t.Errorf("got table %q, want excluded item names/reasons hidden without --explain", out)
	}
}

func TestRenderTableWithExplainListsExcludedItemsAndReasons(t *testing.T) {
	result := engine.Result{
		Excluded: []engine.Excluded{
			{TypeID: 34, Name: "Tritanium", Reason: "thin book"},
			{TypeID: 35, Name: "Pyerite", Reason: "crossed (2\u03b4 \u2265 spread)"},
		},
	}

	out := cli.RenderTable(result, true)

	if !strings.Contains(out, "Tritanium") || !strings.Contains(out, "thin book") {
		t.Errorf("got table %q, want Tritanium and its reason listed under --explain", out)
	}
	if !strings.Contains(out, "Pyerite") || !strings.Contains(out, "crossed") {
		t.Errorf("got table %q, want Pyerite and its reason listed under --explain", out)
	}
}

func TestRenderTableFallsBackToTheTypeIDWhenNameIsNotYetResolved(t *testing.T) {
	// Name resolution (ESI type IDs -> display names) isn't wired into the
	// whole-universe pipeline yet; the table must still identify every
	// item rather than rendering a blank column.
	result := engine.Result{
		BuyRecommendations: []engine.BuyRecommendation{{TypeID: 11399, ExpectedDailyProfit: 1}},
		Excluded:           []engine.Excluded{{TypeID: 34, Reason: "thin book"}},
	}

	out := cli.RenderTable(result, true)

	if !strings.Contains(out, "11399") {
		t.Errorf("got table %q, want the funded item identified by type_id 11399 when its name is empty", out)
	}
	if !strings.Contains(out, "34") {
		t.Errorf("got table %q, want the excluded item identified by type_id 34 when its name is empty", out)
	}
}

func TestRenderTableRendersAnEmptyResultCleanly(t *testing.T) {
	out := cli.RenderTable(engine.Result{}, true)

	if out == "" {
		t.Fatal("got an empty string, want a clean rendering of a zero-candidate run")
	}
	if strings.Contains(out, "NaN") || strings.Contains(out, "Inf") {
		t.Errorf("got table %q, want no NaN/Inf from dividing by a zero budget", out)
	}
}

func TestRenderTableShowsSellRecommendationsWithMarginAndProceeds(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{{
			TypeID: 34, Name: "Tritanium", Quantity: 250, SellPrice: 100,
			NetMargin: 0.42275, PricedQuantity: 250, NetProceeds: 23_100,
		}},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "SELL RECOMMENDATIONS") {
		t.Fatalf("got table %q, want a sell recommendations section", out)
	}
	if !strings.Contains(out, "Tritanium") {
		t.Errorf("got table %q, want the held item named", out)
	}
	if !strings.Contains(out, "42.3%") {
		t.Errorf("got table %q, want the net margin (42.3%%)", out)
	}
	if !strings.Contains(out, "23,100") {
		t.Errorf("got table %q, want the net proceeds", out)
	}
}

func TestRenderTableAnnotatesPartiallyPricedSellRecommendations(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{{
			TypeID: 34, Name: "Tritanium", Quantity: 980, SellPrice: 100,
			NetMargin: 0.42, PricedQuantity: 630, UnpricedQuantity: 350, NetProceeds: 1,
		}},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "630/980u") {
		t.Errorf("got table %q, want the partial-pricing \"X%% on 630/980u\" notation", out)
	}
}

func TestRenderTableFlagsABelowTargetSellRecommendation(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{{
			TypeID: 34, Name: "Tritanium", Quantity: 100, SellPrice: 100,
			NetMargin: 0.01, PricedQuantity: 100, BelowTarget: true, NetProceeds: 1,
		}},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "below target") {
		t.Errorf("got table %q, want the below-target flag shown without hiding the row", out)
	}
}

func TestRenderTableShowsPendingCountsByReasonByDefault(t *testing.T) {
	result := engine.Result{
		Pending: []engine.Pending{
			{TypeID: 1, Name: "A", Reason: engine.PendingAwaitingBuyFill, Detail: "detail one"},
			{TypeID: 2, Name: "B", Reason: engine.PendingAwaitingBuyFill, Detail: "detail two"},
			{TypeID: 3, Name: "C", Reason: engine.PendingUnknownOutcome, Detail: "detail three"},
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "PENDING (3)") {
		t.Fatalf("got table %q, want a pending count of 3", out)
	}
	if !strings.Contains(out, "awaiting-buy-fill 2") || !strings.Contains(out, "unknown-outcome 1") {
		t.Errorf("got table %q, want pending counts by reason", out)
	}
	if strings.Contains(out, "detail one") {
		t.Errorf("got table %q, want pending details hidden without --explain", out)
	}
}

func TestRenderTableWithExplainListsPendingItemsAndDetails(t *testing.T) {
	result := engine.Result{
		Pending: []engine.Pending{{
			TypeID: 3, Name: "Tritanium", Reason: engine.PendingUnknownOutcome,
			Quantity: 40, OrderID: 1002, Detail: "order 1002 vanished with 40 units unaccounted for",
		}},
	}

	out := cli.RenderTable(result, true)

	if !strings.Contains(out, "Tritanium") || !strings.Contains(out, "unknown-outcome") {
		t.Errorf("got table %q, want the pending item and reason listed under --explain", out)
	}
	if !strings.Contains(out, "order 1002 vanished") {
		t.Errorf("got table %q, want the pending detail under --explain", out)
	}
}

// TestRenderTableRendersSellsInTheirWorstShortfallFirstOrder proves the
// renderer presents the sell table in the order RecommendSells established
// (spec §11): worst margin shortfall first. The renderer does not re-sort —
// the order is the data's invariant — so this guards against a future
// renderer reordering the rows.
func TestRenderTableRendersSellsInTheirWorstShortfallFirstOrder(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{
			{TypeID: 34, Name: "WorstFirst", Quantity: 1, NetMargin: 0.01, PricedQuantity: 1, BelowTarget: true},
			{TypeID: 35, Name: "LessBad", Quantity: 1, NetMargin: 0.09, PricedQuantity: 1, BelowTarget: true},
		},
	}

	out := cli.RenderTable(result, false)

	first := strings.Index(out, "WorstFirst")
	second := strings.Index(out, "LessBad")
	if first == -1 || second == -1 {
		t.Fatalf("got table %q, want both sell rows", out)
	}
	if first > second {
		t.Errorf("got LessBad before WorstFirst, want the worst margin shortfall first (spec §11)")
	}
}

// TestRenderTableShowsSellLotCount proves the sell table's last column
// carries how many ledger lots the recommendation draws from (spec §14).
func TestRenderTableShowsSellLotCount(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{{
			TypeID: 34, Name: "Tritanium", Quantity: 980, SellPrice: 812,
			NetMargin: 0.42, PricedQuantity: 980, NetProceeds: 1,
			Lots: []engine.SellLot{{LotID: "a"}, {LotID: "b"}, {LotID: "c"}},
		}},
	}

	out := cli.RenderTable(result, false)

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "Tritanium") {
			continue
		}
		fields := strings.Fields(line)
		if last := fields[len(fields)-1]; last != "3" {
			t.Errorf("got row %q ending in %q, want the lot count 3", line, last)
		}
		return
	}
	t.Fatalf("got table %q, want a Tritanium sell row", out)
}

// TestRenderTableSplitsBudgetAndOrderLimitIntoReservedAvailableAndUsed pins
// spec §14's summary: a second run shouldn't feel like it arbitrarily has
// less room, so both resources are shown split into what pre-existing open
// orders reserved, what was therefore available, and what this run used.
func TestRenderTableSplitsBudgetAndOrderLimitIntoReservedAvailableAndUsed(t *testing.T) {
	result := engine.Result{
		Summary: engine.Summary{
			Budget:                   150_000_000,
			BudgetReservedByExisting: 12_400_000,
			BudgetAvailable:          137_600_000,
			CommittedCapital:         118_300_000,
			OrderLimit:               21,
			OrdersReservedByExisting: 5,
			OrdersAvailable:          16,
			OrdersUsed:               9,
		},
	}

	out := cli.RenderTable(result, false)

	if !strings.Contains(out, "SUMMARY") {
		t.Fatalf("got table %q, want a summary section", out)
	}
	for _, want := range []string{
		"reserved by existing orders: 12,400,000",
		"available: 137,600,000",
		"used this run: 118,300,000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("got table %q, want the budget split %q", out, want)
		}
	}
	for _, want := range []string{
		"reserved by existing orders: 5",
		"available: 16",
		"used this run: 9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("got table %q, want the order-limit split %q", out, want)
		}
	}
}

// TestRenderTableSizesSellColumnsFromTheLongestRowCell pins the prototype's
// hard-won lesson (spec §14): the partial-pricing notation ("X% on Y/Zu") is
// longer than its "net margin" header, so a renderer that sized the margin
// column from the header would shove every column after it out of line. The
// widths must come from the row data, and the row must still carry its lot
// count in a column of its own.
func TestRenderTableSizesSellColumnsFromTheLongestRowCell(t *testing.T) {
	result := engine.Result{
		SellRecommendations: []engine.SellRecommendation{
			{
				TypeID: 34, Name: "Tritanium", Quantity: 980, SellPrice: 812,
				NetMargin: 0.4228, PricedQuantity: 630, UnpricedQuantity: 350,
				BelowTarget: true, NetProceeds: 790_000,
				Lots: []engine.SellLot{{LotID: "a"}, {LotID: "b"}},
			},
			{
				TypeID: 35, Name: "Morphite", Quantity: 100, SellPrice: 24_080,
				NetMargin: 0.183, PricedQuantity: 100, NetProceeds: 2_300_000,
				Lots: []engine.SellLot{{LotID: "c"}},
			},
		},
	}

	out := cli.RenderTable(result, false)
	lines := strings.Split(out, "\n")

	headerIdx, longRowIdx := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "net margin") {
			headerIdx = i
		}
		if strings.Contains(line, "42.3% on 630/980u") {
			longRowIdx = i
		}
	}
	if headerIdx == -1 || longRowIdx == -1 {
		t.Fatalf("got table %q, want a sell-table header and a partially priced row", out)
	}

	// If widths were hardcoded to the header, the partial-pricing cell's
	// extra 7 characters would push the row's flag column 7 columns later.
	headerFlag := strings.Index(lines[headerIdx], "flag")
	rowFlag := strings.Index(lines[longRowIdx], "below target")
	if headerFlag != rowFlag {
		t.Errorf("got flag column at offset %d in the header but %d in the partial-priced row; want the column sized from the longest cell",
			headerFlag, rowFlag)
	}

	// The row still ends with its lot count.
	fields := strings.Fields(lines[longRowIdx])
	if len(fields) == 0 || fields[0] != "Tritanium" {
		t.Fatalf("got row fields %q, want the Tritanium row first", fields)
	}
	if last := fields[len(fields)-1]; last != "2" {
		t.Errorf("got row %q ending in %q, want the lot count 2", lines[longRowIdx], last)
	}
}

// TestRenderTableSizesBuyColumnsFromTheLongestRowCell pins the same
// data-driven discipline for the buy table: an item name longer than any
// fixed guess must widen the name column for the header too, not shove the
// row's later columns out of line.
func TestRenderTableSizesBuyColumnsFromTheLongestRowCell(t *testing.T) {
	longName := "A Very Long Item Name That Overflows Any Fixed Column"
	result := engine.Result{
		BuyRecommendations: []engine.BuyRecommendation{{
			TypeID: 11399, Name: longName, BuyPrice: 1_720_100, SellPrice: 3_697_900,
			NetMargin: 0.4791, Units: 17, CommittedCapital: 30_623_290,
			ExpectedDailyProfit: 10_039_780,
		}},
	}

	out := cli.RenderTable(result, false)
	lines := strings.Split(out, "\n")

	headerIdx, rowIdx := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "ITEM") {
			headerIdx = i
		}
		if strings.Contains(line, longName) {
			rowIdx = i
		}
	}
	if headerIdx == -1 || rowIdx == -1 {
		t.Fatalf("got table %q, want a buy-table header and the long-name row", out)
	}
	// The last column is right-aligned with no trailing padding, so a table
	// sized from the row data has header and row of equal length; a hardcoded
	// name width would let the row run long.
	if len(lines[headerIdx]) != len(lines[rowIdx]) {
		t.Errorf("got header %q (len %d) and row %q (len %d); want equal widths from the row data",
			lines[headerIdx], len(lines[headerIdx]), lines[rowIdx], len(lines[rowIdx]))
	}
}
