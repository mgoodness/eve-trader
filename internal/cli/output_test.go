package cli_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// TestBuildResultAssemblesTheOutputContractFromTheAllocatedUniverse proves
// BuildResult wires AllocatedUniverse's funded/unfunded/excluded sets and
// the live PilotFacts they were computed with into the stable JSON
// contract's shape (spec §11): a Meta echoing the run's params and live
// fees, and a Summary/Recommendations/Unfunded/Excluded consistent with
// what AllocatedUniverse reported.
func TestBuildResultAssemblesTheOutputContractFromTheAllocatedUniverse(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	result, warnings, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}

	if result.Meta.RegionID != cfg.RegionID || result.Meta.TradeStation != cfg.TradeStationID {
		t.Errorf("got Meta=%+v, want RegionID=%d TradeStation=%d", result.Meta, cfg.RegionID, cfg.TradeStationID)
	}
	if result.Meta.Params.Budget != cfg.Values.Budget {
		t.Errorf("got Params.Budget=%v, want %v", result.Meta.Params.Budget, cfg.Values.Budget)
	}
	// pilotSkills(): Trade 4, Broker Relations 4, Accounting 3, zero
	// standings (spec §4) -> broker 1.8%, sales tax 5.025%.
	if result.Meta.Fees.Broker != 0.018 || result.Meta.Fees.SalesTax != 0.05025 {
		t.Errorf("got Fees=%+v, want broker 0.018 sales tax 0.05025", result.Meta.Fees)
	}
	if result.Meta.Params.Accounting != 3 || result.Meta.Params.BrokerRelations != 4 {
		t.Errorf("got Params.Accounting=%d Params.BrokerRelations=%d, want 3 and 4", result.Meta.Params.Accounting, result.Meta.Params.BrokerRelations)
	}

	if result.Summary.Recommendations+result.Summary.Unfunded+result.Summary.Excluded != 3 {
		t.Fatalf("got summary=%+v, want funded+unfunded+excluded to account for all 3 candidates in the fixture", result.Summary)
	}
	// Tritanium (34) is excluded by the book-only stage.
	if result.Summary.Excluded != 1 {
		t.Errorf("got Excluded=%d, want 1", result.Summary.Excluded)
	}

	for _, rec := range result.BuyRecommendations {
		if rec.RoiPerDay == 0 {
			t.Errorf("got recommendation %+v with zero RoiPerDay, want it populated", rec)
		}
	}
}

func TestBuildResultPopulatesNamesFromTheBatchedTypeNameLookup(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	result, warnings, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("got warnings %v, want none", warnings)
	}

	byType := map[int32]engine.BuyRecommendation{}
	for _, rec := range result.BuyRecommendations {
		byType[rec.TypeID] = rec
	}
	if rec, ok := byType[11399]; !ok || rec.Name != "Morphite" {
		t.Errorf("got funded Morphite=%+v (present=%v), want Name=Morphite", rec, ok)
	}

	for _, e := range result.Excluded {
		if e.TypeID == 34 && e.Name != "Tritanium" {
			t.Errorf("got excluded Tritanium Name=%q, want Tritanium", e.Name)
		}
	}
}

func TestBuildResultLeavesAnUnresolvedNameEmptyForTheTypeIDFallback(t *testing.T) {
	// Type 40 is not in fixtureTypeNames, so its Name stays empty and the
	// table renderer falls back to "type 40" (spec §11 name is best-effort).
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	result, _, err := cli.BuildResult(t.Context(), testConfig(t, server.URL))
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	for _, rec := range result.BuyRecommendations {
		if rec.TypeID == 40 && rec.Name != "" {
			t.Errorf("got type 40 Name=%q, want it left empty (unresolved)", rec.Name)
		}
	}
}

// TestBuildResultSplitsEveryTwoSidedTypeAcrossTheThreeGroupsWhenTheBudgetIsTight
// locks the output contract's first invariant (spec §11, §14): a candidate
// that clears the filters but cannot be funded is reported in the unfunded
// group rather than dropped, and funded + unfunded + excluded still accounts
// for every two-sided type. A budget below the minimum order forces both of
// the fixture's survivors out of the funded set.
func TestBuildResultSplitsEveryTwoSidedTypeAcrossTheThreeGroupsWhenTheBudgetIsTight(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 1_000_000 // below the 1M minimum order for both survivors

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	if len(result.BuyRecommendations) != 0 {
		t.Errorf("got %d funded, want 0 with a budget below the minimum order", len(result.BuyRecommendations))
	}
	if len(result.Unfunded) != 2 {
		t.Fatalf("got %d unfunded, want the 2 filtered survivors", len(result.Unfunded))
	}
	if len(result.Excluded) != 1 {
		t.Errorf("got %d excluded, want Tritanium (34)", len(result.Excluded))
	}
	if got := result.Summary.Recommendations + result.Summary.Unfunded + result.Summary.Excluded; got != 3 {
		t.Errorf("got funded+unfunded+excluded=%d, want all 3 two-sided types accounted for", got)
	}
	for _, rec := range result.Unfunded {
		if rec.Units != 0 || rec.CommittedCapital != 0 {
			t.Errorf("got unfunded %+v, want zero units and zero committed capital", rec)
		}
		needed := engine.CapitalNeeded(rec, result.Meta.Fees.Broker, result.Meta.Params.CaptureRate, result.Meta.Params.HorizonDays)
		if needed <= 0 {
			t.Errorf("got unfunded %+v with no capital-needed figure, want a positive one", rec)
		}
	}
}

// sellFixtureServer extends historyFilteredFixtureServer with the character
// ledger routes, so a BuildResult test can seed held stock and assert the
// sell plan it produces. The market routes behave exactly as the shared
// fixture does.
func sellFixtureServer(t *testing.T, orders []map[string]any, history map[int32][]map[string]any, fx ledgerFixture) (*httptest.Server, func() int) {
	t.Helper()
	var tokenHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/orders/history/"):
			json.NewEncoder(w).Encode(fx.history)
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/orders/"):
			json.NewEncoder(w).Encode(fx.orders)
		case strings.HasPrefix(r.URL.Path, "/characters/") && strings.HasSuffix(r.URL.Path, "/assets/"):
			json.NewEncoder(w).Encode(fx.assets)
		case strings.Contains(r.URL.Path, "/history"):
			typeID, _ := strconv.Atoi(r.URL.Query().Get("type_id"))
			json.NewEncoder(w).Encode(history[int32(typeID)])
		case r.URL.Path == "/universe/names/":
			var ids []int32
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Errorf("decoding /universe/names/ request: %v", err)
			}
			resolved := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				if name, ok := fixtureTypeNames[id]; ok {
					resolved = append(resolved, map[string]any{"id": id, "name": name, "category": "inventory_type"})
				}
			}
			json.NewEncoder(w).Encode(resolved)
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.Contains(r.URL.Path, "/orders"):
			w.Header().Set("X-Pages", "1")
			json.NewEncoder(w).Encode(orders)
		case r.URL.Path == "/v2/oauth/token":
			tokenHits.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "rotated-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": pilotSkills()})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() int { return int(tokenHits.Load()) }
}

// cliHeldLot builds a held-unlisted ledger lot for the CLI-level tests.
func cliHeldLot(lotID string, typeID int32, qty int64, price *float64) engine.Lot {
	return engine.Lot{
		LotID:             lotID,
		TypeID:            typeID,
		QuantityTotal:     qty,
		QuantityAvailable: qty,
		AcquisitionPrice:  price,
		Status:            engine.LotHeldUnlisted,
	}
}

// TestBuildResultBuildsSellRecommendationsFromTheLedgerForEveryHeldType
// proves BuildResult reconciles the ledger and emits a sell recommendation
// per held type, priced against the candidate universe's station ask and
// bypassing the buy-side filters entirely: Tritanium (34) is excluded from
// the buy side but is still recommended for sale.
func TestBuildResultBuildsSellRecommendationsFromTheLedgerForEveryHeldType(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	// The Hangar assets confirm exactly the held ledger stock, so
	// reconciliation does not clamp it away.
	server, _ := sellFixtureServer(t, rankedFilteredOrders(), history, ledgerFixture{
		assets: []map[string]any{
			{"item_id": 1, "type_id": 11399, "quantity": 100, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
			{"item_id": 2, "type_id": 34, "quantity": 250, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	})
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	writeLedgerFile(t, cfg, []engine.Lot{
		cliHeldLot("lot-morphite", 11399, 100, engine.Float64Ptr(10000)),
		cliHeldLot("lot-tritanium", 34, 250, engine.Float64Ptr(50)),
	})

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	byType := map[int32]engine.SellRecommendation{}
	for _, rec := range result.SellRecommendations {
		byType[rec.TypeID] = rec
	}
	if len(byType) != 2 {
		t.Fatalf("got sell recommendations %+v, want one each for types 11399 and 34", result.SellRecommendations)
	}

	morphite, ok := byType[11399]
	if !ok {
		t.Fatalf("got no sell recommendation for Morphite (11399)")
	}
	if morphite.Quantity != 100 || morphite.PricedQuantity != 100 {
		t.Errorf("got Morphite quantity=%d priced=%d, want the full 100 held units", morphite.Quantity, morphite.PricedQuantity)
	}
	if morphite.SellPrice != 24080-cfg.Values.Delta {
		t.Errorf("got Morphite sell_price=%v, want station ask 24080 - delta %v", morphite.SellPrice, cfg.Values.Delta)
	}
	if morphite.Name != "Morphite" {
		t.Errorf("got Morphite name=%q, want the resolved name", morphite.Name)
	}
	s := morphite.SellPrice
	b := 10000.0
	wantMargin := (s - b - 0.018*b - 0.018*s - 0.05025*s) / s
	if math.Abs(morphite.NetMargin-wantMargin) > 1e-9 {
		t.Errorf("got Morphite net_margin=%v, want %v (\u00a712)", morphite.NetMargin, wantMargin)
	}

	tritanium, ok := byType[34]
	if !ok {
		t.Fatalf("got no sell recommendation for Tritanium (34) despite its held stock")
	}
	if tritanium.Quantity != 250 {
		t.Errorf("got Tritanium quantity=%d, want the full 250 held units", tritanium.Quantity)
	}

	// The JSON contract carries the new arrays under their pinned keys, and
	// the buy side is now buy_recommendations.
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"sell_recommendations"`) || !strings.Contains(string(body), `"buy_recommendations"`) {
		t.Errorf("got JSON without the buy_recommendations/sell_recommendations keys:\n%s", body)
	}
}

// TestBuildResultSurfacesAnUnknownOutcomeAsPending proves the reconciled
// unknown-outcome note reaches the output contract's pending bucket.
func TestBuildResultSurfacesAnUnknownOutcomeAsPending(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, _ := historyFilteredFixtureServer(t, rankedFilteredOrders(), history)
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	// An open-buy lot whose order the (empty) character-orders snapshot no
	// longer shows, with a non-zero last-seen remainder: ESI cannot resolve
	// its fate, so it must surface as unknown-outcome rather than be guessed.
	writeLedgerFile(t, cfg, []engine.Lot{{
		LotID:                "lot-unknown",
		TypeID:               11399,
		SourceOrderID:        "1002",
		QuantityTotal:        100,
		LastSeenVolumeRemain: 40,
		Status:               engine.LotOpenBuy,
	}})

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	var unknown *engine.Pending
	for i := range result.Pending {
		if result.Pending[i].Reason == engine.PendingUnknownOutcome {
			unknown = &result.Pending[i]
		}
	}
	if unknown == nil {
		t.Fatalf("got pending %+v, want an unknown-outcome entry", result.Pending)
	}
	if unknown.TypeID != 11399 || unknown.OrderID != 1002 || unknown.Quantity != 40 {
		t.Errorf("got unknown-outcome %+v, want type 11399, order 1002, quantity 40", *unknown)
	}
	if len(result.SellRecommendations) != 0 {
		t.Errorf("got sell recommendations %+v, want none for a lot in flight", result.SellRecommendations)
	}
	// The unknown-outcome lot is absent from /orders/, so it reserves
	// neither a slot nor budget (spec §13 step 1; ADR 0003): the run still
	// has the pilot's full headroom.
	if result.Summary.OrdersReservedByExisting != 0 || result.Summary.OrdersAvailable != result.Summary.OrderLimit {
		t.Errorf("got orders reserved=%d available=%d, want 0 reserved and %d available",
			result.Summary.OrdersReservedByExisting, result.Summary.OrdersAvailable, result.Summary.OrderLimit)
	}
	if result.Summary.BudgetReservedByExisting != 0 || result.Summary.BudgetAvailable != float64(cfg.Values.Budget) {
		t.Errorf("got budget reserved=%v available=%v, want 0 reserved and %d available",
			result.Summary.BudgetReservedByExisting, result.Summary.BudgetAvailable, cfg.Values.Budget)
	}
}

// TestBuildResultMintsOnlyOneAccessTokenPerRun pins decision 6: the ledger
// reconciliation reuses the buy pipeline's already-minted access token,
// rather than triggering a second refresh exchange that would replay a
// rotated refresh token.
func TestBuildResultMintsOnlyOneAccessTokenPerRun(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	server, tokenHits := sellFixtureServer(t, rankedFilteredOrders(), history, ledgerFixture{})
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	if _, _, err := cli.BuildResult(t.Context(), cfg); err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if got := tokenHits(); got != 1 {
		t.Errorf("got %d access-token exchanges, want exactly 1 per run", got)
	}
}

// openOrderMap builds one wire-shaped open character order for the fake
// /characters/{id}/orders/ route.
func openOrderMap(orderID int64, typeID int32, isBuy bool, price float64, volumeRemain int64) map[string]any {
	return map[string]any{
		"order_id":      orderID,
		"type_id":       typeID,
		"location_id":   60004588,
		"volume_total":  volumeRemain,
		"volume_remain": volumeRemain,
		"min_volume":    1,
		"price":         price,
		"is_buy_order":  isBuy,
		"range":         "station",
	}
}

// TestBuildResultReservesSlotsAndBuyEscrowForPreExistingOpenOrders pins spec
// §13 steps 1–2: every currently-open order (any origin) reserves one
// order-limit slot, and every open buy order's current escrow
// (price × volume_remain) reserves budget. An open sell order carries no
// escrow, so it reserves a slot but no budget.
func TestBuildResultReservesSlotsAndBuyEscrowForPreExistingOpenOrders(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	orders := []map[string]any{
		openOrderMap(9001, 34, true, 200, 50),   // open buy: 10,000 escrow
		openOrderMap(9002, 35, false, 300, 100), // open sell: no escrow
		openOrderMap(9003, 36, true, 1000, 5),   // open buy: 5,000 escrow
	}
	server, _ := sellFixtureServer(t, rankedFilteredOrders(), history, ledgerFixture{orders: orders})
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	if result.Summary.OrdersReservedByExisting != 3 {
		t.Errorf("got OrdersReservedByExisting=%d, want 3 (every open order reserves a slot)", result.Summary.OrdersReservedByExisting)
	}
	if result.Summary.OrdersAvailable != result.Summary.OrderLimit-3 {
		t.Errorf("got OrdersAvailable=%d, want %d", result.Summary.OrdersAvailable, result.Summary.OrderLimit-3)
	}
	// Only the two open buys escrow budget: 200×50 + 1000×5 = 15,000.
	if result.Summary.BudgetReservedByExisting != 15_000 {
		t.Errorf("got BudgetReservedByExisting=%v, want 15000 (open sells carry no escrow)", result.Summary.BudgetReservedByExisting)
	}
	if result.Summary.BudgetAvailable != float64(cfg.Values.Budget)-15_000 {
		t.Errorf("got BudgetAvailable=%v, want %v", result.Summary.BudgetAvailable, float64(cfg.Values.Budget)-15_000)
	}
}

// TestBuildResultMarksASellPendingWhenPreExistingOrdersExhaustTheOrderLimit
// pins spec §13 step 3 and §11: with every order-limit slot already held by
// pre-existing open orders, the sell recommendation can't get one and lands
// in Pending with reason order-limit-exhausted rather than being dropped.
func TestBuildResultMarksASellPendingWhenPreExistingOrdersExhaustTheOrderLimit(t *testing.T) {
	history := map[int32][]map[string]any{
		11399: historyDays(30, 100, 30000, 15000),
		40:    historyDays(30, 1000, 3000, 500),
	}
	// pilotSkills() derives an order limit of 21; 21 open orders leave none.
	orders := make([]map[string]any, 0, 21)
	for i := range 21 {
		orders = append(orders, openOrderMap(int64(9000+i), 34, true, 10, 10))
	}
	server, _ := sellFixtureServer(t, rankedFilteredOrders(), history, ledgerFixture{
		orders: orders,
		assets: []map[string]any{
			{"item_id": 1, "type_id": 34, "quantity": 100, "location_id": 60004588, "location_type": "station", "location_flag": "Hangar"},
		},
	})
	cfg := testConfig(t, server.URL)
	cfg.Values.Budget = 150_000_000
	writeLedgerFile(t, cfg, []engine.Lot{cliHeldLot("lot-34", 34, 100, engine.Float64Ptr(50))})

	result, _, err := cli.BuildResult(t.Context(), cfg)
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}

	if result.Summary.OrdersAvailable != 0 {
		t.Errorf("got OrdersAvailable=%d, want 0 with the limit fully reserved", result.Summary.OrdersAvailable)
	}
	if len(result.SellRecommendations) != 0 {
		t.Errorf("got SellRecommendations=%+v, want none once the order limit is exhausted", result.SellRecommendations)
	}
	var starved *engine.Pending
	for i := range result.Pending {
		if result.Pending[i].TypeID == 34 && result.Pending[i].Reason == engine.PendingOrderLimit {
			starved = &result.Pending[i]
		}
	}
	if starved == nil {
		t.Fatalf("got Pending=%+v, want the starved sell marked order-limit-exhausted", result.Pending)
	}
	if starved.Quantity != 100 || starved.Detail == "" {
		t.Errorf("got %+v, want the full 100 held units and a detail", *starved)
	}
}
