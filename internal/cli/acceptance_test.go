package cli_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// This file is the v1 acceptance check (ticket #24, spec §14): it runs the
// whole CLI pipeline (cli.BuildResult) against ONE frozen, committed live
// snapshot of the Heimatar region-orders feed, with the pilot's spec §4
// skills served as fixtures, and asserts the five functional acceptance
// criteria. It is hermetic and offline: every byte the pipeline reads comes
// from testdata/acceptance (see its README for provenance and what is
// frozen vs synthesized), and nothing here makes a live network call.
//
// It deliberately does NOT go through a live SSO token: the pilot facts are
// frozen at Trade 4 / Broker Relations 4 / Accounting 3 (spec §4),
// reproduced by the fake server's skills fixture. It makes no claim that
// the engine's 20% capture rate is correct (spec §14).

// acceptanceDefaultBudget is the pilot's documented default budget (spec
// §13, config.Defaults): the canonical run this check verifies.
const acceptanceDefaultBudget int64 = 150_000_000

// acceptanceSlotBindingBudget is large enough that the 21-order limit — not
// the budget — is the binding constraint, so the order-limit criterion is
// exercised non-trivially rather than passing because only one candidate
// was funded.
const acceptanceSlotBindingBudget int64 = 10_000_000_000

// acceptanceSnapshot is the frozen input loaded from testdata/acceptance: the
// region-orders feed (split into transport pages exactly as ESI paginates),
// the per-type market history, and the jump-distance routes. twoSided is the
// snapshot's own two-sided universe, computed independently of the pipeline
// so the three-way-split criterion has an expected denominator.
type acceptanceSnapshot struct {
	orderPages [][]byte
	history    map[string]json.RawMessage
	routes     map[string][]int32

	twoSided []engine.CandidateType
}

// loadAcceptanceSnapshot reads and decodes the committed snapshot.
func loadAcceptanceSnapshot(t *testing.T) *acceptanceSnapshot {
	t.Helper()

	snap := &acceptanceSnapshot{
		history: map[string]json.RawMessage{},
		routes:  map[string][]int32{},
	}

	// The feed is committed compacted and split into pages small enough for
	// the repo's large-file hook; serving them as ESI pages exercises the
	// real multi-page fetch/decode path.
	orderPagePaths, err := filepath.Glob(filepath.Join("testdata", "acceptance", "orders-page-*.json.gz"))
	if err != nil {
		t.Fatalf("globbing frozen order pages: %v", err)
	}
	if len(orderPagePaths) == 0 {
		t.Fatal("frozen snapshot has no order pages")
	}
	var orders []engine.Order
	for _, path := range orderPagePaths {
		page := mustReadGzip(t, path)
		snap.orderPages = append(snap.orderPages, page)
		decoded, err := esi.DecodeOrders(page)
		if err != nil {
			t.Fatalf("decoding frozen region-orders page %s: %v", path, err)
		}
		orders = append(orders, decoded...)
	}

	historyJSON := mustReadGzip(t, filepath.Join("testdata", "acceptance", "history.json.gz"))
	if err := json.Unmarshal(historyJSON, &snap.history); err != nil {
		t.Fatalf("decoding frozen history: %v", err)
	}

	routesJSON := mustReadFile(t, filepath.Join("testdata", "acceptance", "routes.json"))
	if err := json.Unmarshal(routesJSON, &snap.routes); err != nil {
		t.Fatalf("decoding frozen routes: %v", err)
	}

	cfg := cli.DefaultConfig()
	jumpDistances := make(map[int32]int, len(snap.routes)+1)
	jumpDistances[cfg.TradeSystemID] = 0
	for system, route := range snap.routes {
		var id int32
		if _, err := fmt.Sscanf(system, "%d", &id); err != nil {
			t.Fatalf("parsing frozen route key %q: %v", system, err)
		}
		jumpDistances[id] = len(route) - 1
	}

	snap.twoSided = engine.TwoSided(engine.Universe(orders, cfg.TradeStationID, cfg.TradeSystemID, jumpDistances))
	if len(snap.twoSided) == 0 {
		t.Fatal("frozen snapshot has no two-sided types; it is empty or corrupt")
	}
	return snap
}

// acceptanceServer serves the frozen snapshot over HTTP in exactly the ESI
// wire formats the esi client expects, plus the frozen pilot facts (a fake
// SSO token, skills, and empty standings), so the unmodified pipeline can
// run with a base URL pointed at it. It never touches the network.
func acceptanceServer(t *testing.T, snap *acceptanceSnapshot) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.HasSuffix(r.URL.Path, "/orders/"):
			// Serve the frozen feed page the request asked for, advertising
			// the committed page count exactly like ESI's X-Pages.
			w.Header().Set("X-Pages", strconv.Itoa(len(snap.orderPages)))
			w.Header().Set("ETag", `"acceptance-orders"`)
			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= len(snap.orderPages) {
					page = n
				}
			}
			w.Write(snap.orderPages[page-1])
		case strings.HasPrefix(r.URL.Path, "/markets/") && strings.HasSuffix(r.URL.Path, "/history/"):
			body, ok := snap.history[r.URL.Query().Get("type_id")]
			if !ok {
				// A history request the snapshot doesn't cover would silently
				// change the pipeline's outcomes; fail loudly instead.
				t.Errorf("frozen snapshot has no history for type_id=%s", r.URL.Query().Get("type_id"))
				http.NotFound(w, r)
				return
			}
			w.Write(body)
		case r.URL.Path == "/universe/names/":
			// The acceptance check does not exercise names, and the snapshot
			// holds no name data, so serve a deterministic placeholder for
			// every requested id. This keeps the name lookup's warnings out
			// of the market-mechanics assertions.
			var ids []int32
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Errorf("decoding /universe/names/ request: %v", err)
			}
			resolved := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				resolved = append(resolved, map[string]any{"id": id, "name": fmt.Sprintf("type %d", id), "category": "inventory_type"})
			}
			json.NewEncoder(w).Encode(resolved)
		case strings.HasPrefix(r.URL.Path, "/route/"):
			segment := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(segment) != 3 {
				http.NotFound(w, r)
				return
			}
			route, ok := snap.routes[segment[1]]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(route)
		case r.URL.Path == "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "acceptance-rotated-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": pilotSkills()})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// acceptanceConfig is the run configuration for the acceptance check: the
// frozen snapshot's base URL, a fresh on-disk cache (so each invocation
// exercises the fetch/decode path), the frozen pilot's credentials
// placeholder (never real), and the given budget.
func acceptanceConfig(t *testing.T, serverURL string, budget int64) cli.Config {
	t.Helper()

	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = serverURL
	cfg.SSOBaseURL = serverURL
	cfg.CacheDir = t.TempDir()
	cfg.Values.Budget = budget
	cfg.Credentials = config.Credentials{ClientID: "acceptance-client", RefreshToken: "acceptance-refresh-token"}
	return cfg
}

// runAcceptanceOnce runs the pipeline once against the frozen snapshot and
// returns the Result. A non-empty warnings slice means the snapshot is
// incomplete (a route lookup missed), which the caller treats as a failure.
func runAcceptanceOnce(t *testing.T, ctx context.Context, serverURL string, budget int64) engine.Result {
	t.Helper()

	result, warnings, err := cli.BuildResult(ctx, acceptanceConfig(t, serverURL, budget))
	if err != nil {
		t.Fatalf("BuildResult against frozen snapshot: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("pipeline reported warnings against the frozen snapshot, want none: %v", warnings)
	}
	return result
}

func TestAcceptanceAgainstFrozenSnapshot(t *testing.T) {
	snap := loadAcceptanceSnapshot(t)
	server := acceptanceServer(t, snap)

	// The canonical run: the pilot's documented default budget (spec §13).
	result := runAcceptanceOnce(t, t.Context(), server.URL, acceptanceDefaultBudget)
	t.Logf("frozen snapshot: two-sided=%d, funded=%d, unfunded=%d, excluded=%d, committed=%.0f, orders=%d/%d (budget %d)",
		len(snap.twoSided), len(result.Recommendations), len(result.Unfunded), len(result.Excluded),
		result.Summary.CommittedCapital, result.Summary.OrdersUsed, result.Summary.OrderLimit, result.Summary.Budget)

	t.Run("every funded recommendation is postable", func(t *testing.T) {
		assertPostable(t, result, snap.twoSided)
	})

	t.Run("every funded recommendation clears the target net margin at the pilot's real skills and standings", func(t *testing.T) {
		assertClearsTargetMargin(t, result)
	})

	t.Run("the budget and the order limit are respected", func(t *testing.T) {
		assertBudgetAndOrderLimit(t, result)
		if len(result.Recommendations) == 0 {
			t.Fatal("frozen snapshot funded nothing at the default budget; the acceptance check would be vacuous")
		}
	})

	t.Run("funded plus unfunded plus excluded accounts for every two-sided type", func(t *testing.T) {
		assertThreeWaySplit(t, result, snap.twoSided)
	})

	t.Run("the order limit binds and is respected under a larger budget", func(t *testing.T) {
		result := runAcceptanceOnce(t, t.Context(), server.URL, acceptanceSlotBindingBudget)
		t.Logf("slot-binding run: funded=%d, unfunded=%d, excluded=%d, orders=%d/%d",
			len(result.Recommendations), len(result.Unfunded), len(result.Excluded), result.Summary.OrdersUsed, result.Summary.OrderLimit)
		// Ten funded candidates (20 of the 21 slots) is the honest way to
		// exercise the limit: with more budget available than slots, the
		// limit — not the budget — is what caps the funded set. The same
		// structural criteria are re-asserted here because this run funds
		// far more candidates than the default-budget run.
		assertPostable(t, result, snap.twoSided)
		assertClearsTargetMargin(t, result)
		assertBudgetAndOrderLimit(t, result)
		assertThreeWaySplit(t, result, snap.twoSided)
		if result.Summary.OrdersUsed+2 <= result.Summary.OrderLimit {
			t.Errorf("got OrdersUsed=%d of limit %d: the slot-binding scenario did not actually bind, so the order-limit check is vacuous",
				result.Summary.OrdersUsed, result.Summary.OrderLimit)
		}
	})

	t.Run("the check is repeatable against the committed snapshot", func(t *testing.T) {
		again := runAcceptanceOnce(t, t.Context(), server.URL, acceptanceDefaultBudget)
		assertRepeatable(t, result, again)
	})
}

// assertPostable checks the first acceptance criterion (spec §14): every
// funded recommendation's front-of-queue prices beat the snapshot's own
// best bid/ask — buy strictly above the best bid, sell strictly below the
// best ask. The best bid/ask come from the snapshot's independently
// computed two-sided universe, not from the recommendation's own fields, so
// a pricing bug cannot satisfy both sides.
func assertPostable(t *testing.T, result engine.Result, twoSided []engine.CandidateType) {
	t.Helper()

	universe := map[int32]engine.CandidateType{}
	for _, c := range twoSided {
		universe[c.TypeID] = c
	}

	for _, rec := range result.Recommendations {
		c, ok := universe[rec.TypeID]
		if !ok {
			t.Errorf("funded type %d is not in the snapshot's two-sided universe", rec.TypeID)
			continue
		}
		bestBid := maxPrice(c.BuyBook)
		bestAsk := minPrice(c.SellBook)
		if !(rec.BuyPrice > bestBid) {
			t.Errorf("type %d: buy price %.2f is not above best bid %.2f", rec.TypeID, rec.BuyPrice, bestBid)
		}
		if !(rec.SellPrice < bestAsk) {
			t.Errorf("type %d: sell price %.2f is not below best ask %.2f", rec.TypeID, rec.SellPrice, bestAsk)
		}
	}
}

// assertClearsTargetMargin checks the second acceptance criterion (spec
// §14): every funded recommendation clears the run's target net margin,
// recomputed from its posted prices at the pilot's frozen fee rates. It
// also pins the frozen pilot facts themselves (broker 1.8%, sales tax
// 5.025%, order limit 21) so the check cannot silently pass at different
// skills.
func assertClearsTargetMargin(t *testing.T, result engine.Result) {
	t.Helper()

	if result.Meta.Fees.Broker != 0.018 || result.Meta.Fees.SalesTax != 0.05025 {
		t.Fatalf("got fees %+v, want the pilot's frozen broker 1.8%% / sales tax 5.025%%", result.Meta.Fees)
	}
	if result.Meta.Params.BrokerRelations != 4 || result.Meta.Params.Accounting != 3 {
		t.Fatalf("got BrokerRelations=%d Accounting=%d, want the pilot's frozen 4 and 3",
			result.Meta.Params.BrokerRelations, result.Meta.Params.Accounting)
	}

	target := result.Meta.Params.TargetMargin
	if target <= 0 {
		t.Fatalf("got target margin %v, want a positive configured target", target)
	}

	for _, rec := range result.Recommendations {
		// The broker fee is charged on each order leg with a 100 ISK minimum
		// per order (spec §4, §8). Pricing works per unit, so each leg is
		// floored at 100 ISK, matching engine.Price.
		brokerBuy := math.Max(result.Meta.Fees.Broker*rec.BuyPrice, 100)
		brokerSell := math.Max(result.Meta.Fees.Broker*rec.SellPrice, 100)
		recomputed := (rec.SellPrice - rec.BuyPrice - brokerBuy - brokerSell -
			result.Meta.Fees.SalesTax*rec.SellPrice) / rec.SellPrice
		if recomputed < target-1e-9 {
			t.Errorf("type %d: recomputed net margin %.6f is below the target %.6f", rec.TypeID, recomputed, target)
		}
		if rec.NetMargin < target-1e-9 {
			t.Errorf("type %d: reported net margin %.6f is below the target %.6f", rec.TypeID, rec.NetMargin, target)
		}
	}
}

// assertBudgetAndOrderLimit checks the third acceptance criterion (spec
// §14): committed capital never exceeds the budget, and the funded set's
// active-order cost (two slots per candidate, spec §10) never exceeds the
// pilot's skill-derived order limit.
func assertBudgetAndOrderLimit(t *testing.T, result engine.Result) {
	t.Helper()

	if result.Summary.OrderLimit != 21 {
		t.Errorf("got order limit %d, want the pilot's frozen 21", result.Summary.OrderLimit)
	}
	if result.Summary.OrdersUsed != 2*len(result.Recommendations) {
		t.Errorf("got OrdersUsed=%d for %d funded recommendations, want %d (two slots each)",
			result.Summary.OrdersUsed, len(result.Recommendations), 2*len(result.Recommendations))
	}
	if result.Summary.OrdersUsed > result.Summary.OrderLimit {
		t.Errorf("got OrdersUsed=%d, exceeding the order limit %d", result.Summary.OrdersUsed, result.Summary.OrderLimit)
	}
	if result.Summary.CommittedCapital > float64(result.Summary.Budget)+1e-6 {
		t.Errorf("got committed capital %.2f, exceeding the budget %d", result.Summary.CommittedCapital, result.Summary.Budget)
	}

	var committed float64
	for _, rec := range result.Recommendations {
		committed += rec.CommittedCapital
	}
	if math.Abs(committed-result.Summary.CommittedCapital) > 1 {
		t.Errorf("sum of funded CommittedCapital %.2f disagrees with Summary.CommittedCapital %.2f", committed, result.Summary.CommittedCapital)
	}
}

// assertThreeWaySplit checks the fourth acceptance criterion (spec §14):
// every two-sided type lands in exactly one of funded / unfunded / excluded
// — nothing is dropped and nothing is invented. The denominator is the
// snapshot's own two-sided universe (engine.TwoSided over the committed
// feed), not the pipeline's own counts.
func assertThreeWaySplit(t *testing.T, result engine.Result, twoSided []engine.CandidateType) {
	t.Helper()

	if got := len(result.Recommendations) + len(result.Unfunded) + len(result.Excluded); got != len(twoSided) {
		t.Errorf("got funded+unfunded+excluded=%d, want every two-sided type (%d)", got, len(twoSided))
	}

	placements := map[int32]int{}
	for _, rec := range result.Recommendations {
		placements[rec.TypeID]++
	}
	for _, rec := range result.Unfunded {
		placements[rec.TypeID]++
	}
	for _, e := range result.Excluded {
		placements[e.TypeID]++
	}

	want := map[int32]bool{}
	for _, c := range twoSided {
		want[c.TypeID] = true
	}
	for typeID := range want {
		if placements[typeID] != 1 {
			t.Errorf("two-sided type %d appears in %d of the three groups, want exactly 1", typeID, placements[typeID])
		}
	}
	for typeID, n := range placements {
		if !want[typeID] {
			t.Errorf("type %d appears in the output (%d times) but is not two-sided in the snapshot", typeID, n)
		}
	}
}

// assertRepeatable checks the fifth acceptance criterion (spec §14): two
// runs against the same committed snapshot — each with its own fresh cache,
// so both exercise the fetch/decode path — produce the same substantive
// result. GeneratedAt is the only field expected to differ.
func assertRepeatable(t *testing.T, first, second engine.Result) {
	t.Helper()

	first.Meta.GeneratedAt = second.Meta.GeneratedAt
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two runs against the committed snapshot disagree:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func maxPrice(book []engine.Order) float64 {
	best := book[0].Price
	for _, o := range book[1:] {
		if o.Price > best {
			best = o.Price
		}
	}
	return best
}

func minPrice(book []engine.Order) float64 {
	best := book[0].Price
	for _, o := range book[1:] {
		if o.Price < best {
			best = o.Price
		}
	}
	return best
}

// mustReadGzip reads a gzip-compressed file and returns its decompressed
// contents.
func mustReadGzip(t *testing.T, path string) []byte {
	t.Helper()
	compressed := mustReadFile(t, path)
	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decompressing %s: %v", path, err)
	}
	return body
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return body
}
