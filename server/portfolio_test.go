package server_test

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

const (
	portfolioCharacterID = 123
	portfolioRens        = 60004588
)

// seedPortfolioTx inserts one wallet_transaction row.
func seedPortfolioTx(t *testing.T, db *sql.DB, txID int64, date time.Time, typeID, quantity int, unitPrice float64, isBuy bool) {
	t.Helper()
	buy := 0
	if isBuy {
		buy = 1
	}
	if _, err := db.Exec(`
		INSERT INTO wallet_transaction (transaction_id, date, type_id, quantity, unit_price, is_buy, is_personal, journal_ref_id, location_id, client_id)
		VALUES (?, ?, ?, ?, ?, ?, 1, 0, ?, 0)`,
		txID, date.UTC().Format(time.RFC3339Nano), typeID, quantity, unitPrice, buy, portfolioRens,
	); err != nil {
		t.Fatalf("seeding wallet_transaction %d: %v", txID, err)
	}
}

// seedPortfolioOrder inserts one (order_id, issued) character_order snapshot.
func seedPortfolioOrder(t *testing.T, db *sql.DB, orderID int64, issued time.Time, typeID int, isBuy bool, price float64, volumeRemain, volumeTotal int, state string) {
	t.Helper()
	buy := 0
	if isBuy {
		buy = 1
	}
	if _, err := db.Exec(`
		INSERT INTO character_order (order_id, issued, type_id, location_id, is_buy_order, price, volume_remain, volume_total,
			min_volume, duration, state, is_corporation, region_id, order_range, escrow, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 90, ?, 0, 0, 'station', 0, '2024-01-01T00:00:00Z')`,
		orderID, issued.UTC().Format(time.RFC3339Nano), typeID, portfolioRens, buy, price, volumeRemain, volumeTotal, state,
	); err != nil {
		t.Fatalf("seeding character_order %d: %v", orderID, err)
	}
}

// seedPortfolioJournal inserts the journal fee/tax entries the summary
// strip reconciles against. Amounts are ESI-style expenses (negative).
func seedPortfolioJournal(t *testing.T, db *sql.DB, entries ...[2]any) {
	t.Helper()
	for i, e := range entries {
		id := int64(5000 + i)
		if _, err := db.Exec(`
			INSERT INTO wallet_journal (id, date, ref_type, amount, balance, description)
			VALUES (?, '2024-01-01T00:00:00Z', ?, ?, 0, '')`,
			id, e[0].(string), e[1].(float64),
		); err != nil {
			t.Fatalf("seeding wallet_journal %d: %v", id, err)
		}
	}
}

// seedPortfolioContract inserts a finished item-exchange contract in which
// the character is the issuer, so its items leave the character.
func seedPortfolioContract(t *testing.T, db *sql.DB, contractID, issuerID int64, price float64, issued time.Time) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO contract (contract_id, issuer_id, issuer_corporation_id, assignee_id, acceptor_id,
			type, status, price, for_corporation, date_issued, date_expired, start_location_id, updated_at)
		VALUES (?, ?, 1, 0, 0, 'item_exchange', 'finished', ?, 0, ?, ?, ?, '2024-01-01T00:00:00Z')`,
		contractID, issuerID, price,
		issued.UTC().Format(time.RFC3339Nano), issued.AddDate(0, 0, 7).UTC().Format(time.RFC3339Nano), portfolioRens,
	); err != nil {
		t.Fatalf("seeding contract %d: %v", contractID, err)
	}
}

// seedPortfolioFixtures seeds the five decision groups over the P/L engine:
// an at-target and a below-target position, a no-market position, a fully
// sold (closed) position, and a fully transferred position. R_b = 3%,
// R_t = 7.5% at Broker Relations 0 / Accounting 0.
func seedPortfolioFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	dbtest.SeedToken(t, db, portfolioCharacterID, testAuthConfig().TokenKey, "refresh-token")
	dbtest.SeedSkills(t, db, portfolioCharacterID, 0, 0)

	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	day5 := time.Date(2024, 1, 5, 10, 0, 0, 0, time.UTC)

	// At target now: bought at 100, best sell 200 => +76,000 unrealized.
	dbtest.SeedItem(t, db, 100, "At Target Ore")
	seedPortfolioTx(t, db, 1, day1, 100, 1000, 100, true)
	seedPortfolioOrder(t, db, 100, day1, 100, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 1000, 100, true, 190)
	dbtest.SeedOrder(t, db, 1001, 100, false, 200)

	// Below target: bought at 100, best sell 102 => -11,710 unrealized.
	dbtest.SeedItem(t, db, 101, "Below Target Ore")
	seedPortfolioTx(t, db, 2, day1, 101, 1000, 100, true)
	seedPortfolioOrder(t, db, 101, day1, 101, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 1010, 101, true, 101)
	dbtest.SeedOrder(t, db, 1011, 101, false, 102)

	// No market: held with no Rens book.
	dbtest.SeedItem(t, db, 102, "No Market Ore")
	seedPortfolioTx(t, db, 3, day1, 102, 1000, 100, true)
	seedPortfolioOrder(t, db, 102, day1, 102, true, 100, 0, 1000, "")

	// Closed: bought at 100 and fully sold at 150 => +31,250 realized.
	dbtest.SeedItem(t, db, 103, "Closed Ore")
	seedPortfolioTx(t, db, 4, day1, 103, 1000, 100, true)
	seedPortfolioOrder(t, db, 103, day1, 103, true, 100, 0, 1000, "")
	seedPortfolioTx(t, db, 5, day2, 103, 1000, 150, false)
	seedPortfolioOrder(t, db, 1030, day2, 103, false, 150, 0, 1000, "")

	// Transferred: bought at 100 and fully transferred by contract at cost.
	dbtest.SeedItem(t, db, 104, "Transferred Ore")
	seedPortfolioTx(t, db, 6, day1, 104, 1000, 100, true)
	seedPortfolioOrder(t, db, 104, day1, 104, true, 100, 0, 1000, "")
	seedPortfolioContract(t, db, 900, portfolioCharacterID, 0, day5)
	if _, err := db.Exec(`INSERT INTO contract_item (contract_id, record_id, type_id, quantity, is_singleton, is_included)
		VALUES (900, 1, 104, 1000, 0, 1)`); err != nil {
		t.Fatalf("seeding contract_item: %v", err)
	}

	// Journal fees reconcile exactly with the engine's estimates, so the
	// unattributed bucket is zero.
	seedPortfolioJournal(t, db,
		[2]any{"brokers_fee", -3000.0},      // 100 buy
		[2]any{"brokers_fee", -3000.0},      // 101 buy
		[2]any{"brokers_fee", -3000.0},      // 102 buy
		[2]any{"brokers_fee", -3000.0},      // 103 buy
		[2]any{"brokers_fee", -4500.0},      // 103 sell
		[2]any{"transaction_tax", -11250.0}, // 103 sell tax
		[2]any{"brokers_fee", -3000.0},      // 104 buy
	)
}

func renderPortfolio(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()
	status, body := getBody(t, srv.URL+"/portfolio")
	if status != 200 {
		t.Fatalf("GET /portfolio status = %d, want 200", status)
	}
	return body
}

// TestPortfolioRendersGroupsColumnsSummaryAndFootnote is the ticket's
// headline acceptance test: the seeded ledger renders the five decision
// groups, the spec's column set, the four-cell summary strip, and the
// estimates footnote.
func TestPortfolioRendersGroupsColumnsSummaryAndFootnote(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedPortfolioFixtures(t, sqlDB)
	body := renderPortfolio(t, sqlDB)

	for _, want := range []string{"At target now", "Below target", "Transfers", "No market", "Closed / realized"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing group %q", want)
		}
	}

	for _, want := range []string{">Item / location<", ">Qty<", ">Avg cost<", ">Mkt sell<", ">Unreal P/L<", ">Realized<", ">Buy / sell re-lists<", ">Est. fees<", ">Break-even<", ">Target<", ">Mkt net margin<", ">Status<"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing column %q", want)
		}
	}

	// Summary strip, computed by the real engine: realized 31,250,
	// unrealized 64,290 (76,000 - 11,710), estimated fees 30,750, and a
	// zero unattributed bucket because the journal reconciles.
	for _, want := range []string{"Realized", "31,250 ISK", "Unrealized", "64,290 ISK", "Fees (est)", "30,750 ISK", "Unattributed", "0 ISK"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing summary value %q", want)
		}
	}

	for _, want := range []string{"estimated", "Per-item fees are estimates", "Unattributed fees are mostly re-lists", "standings toward the Rens station owner"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing footnote/legend disclosure %q", want)
		}
	}
}

// TestPortfolioGroupsPositionsByDecision asserts each seeded item lands in
// the decision group its status names, not merely that the headings render.
func TestPortfolioGroupsPositionsByDecision(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedPortfolioFixtures(t, sqlDB)
	body := renderPortfolio(t, sqlDB)

	groups := []struct {
		heading string
		item    string
	}{
		{"At target now", "At Target Ore"},
		{"Below target", "Below Target Ore"},
		{"Transfers", "Transferred Ore"},
		{"No market", "No Market Ore"},
		{"Closed / realized", "Closed Ore"},
	}
	for _, g := range groups {
		headingIdx := strings.Index(body, g.heading)
		itemIdx := strings.Index(body, g.item)
		if headingIdx == -1 || itemIdx == -1 {
			t.Fatalf("body missing heading %q or item %q", g.heading, g.item)
		}
		if itemIdx < headingIdx {
			t.Errorf("item %q rendered before its group heading %q", g.item, g.heading)
		}
	}
}

// seedPortfolioForecastFixtures seeds three open positions whose allocated
// fees (3,000 / 6,000 / 3,000) differ, plus a journal that carries 1,200 ISK
// more in broker fees than the engine can attribute. That 1,200 ISK
// unattributed bucket is shared across the positions for the forecast's high
// end, so the break-even/target columns show a real low/high range.
func seedPortfolioForecastFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	dbtest.SeedToken(t, db, portfolioCharacterID, testAuthConfig().TokenKey, "refresh-token")
	dbtest.SeedSkills(t, db, portfolioCharacterID, 0, 0)

	day1 := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

	dbtest.SeedItem(t, db, 200, "Range A")
	seedPortfolioTx(t, db, 100, day1, 200, 1000, 100, true)
	seedPortfolioOrder(t, db, 200, day1, 200, true, 100, 0, 1000, "")
	dbtest.SeedOrder(t, db, 2200, 200, false, 120)

	dbtest.SeedItem(t, db, 201, "Range B")
	seedPortfolioTx(t, db, 101, day1, 201, 1000, 200, true)
	seedPortfolioOrder(t, db, 201, day1, 201, true, 200, 0, 1000, "")
	dbtest.SeedOrder(t, db, 2201, 201, false, 220)

	dbtest.SeedItem(t, db, 202, "Range C")
	seedPortfolioTx(t, db, 102, day1, 202, 1000, 100, true)
	seedPortfolioOrder(t, db, 202, day1, 202, true, 100, 0, 1000, "")

	seedPortfolioJournal(t, db,
		[2]any{"brokers_fee", -3000.0},
		[2]any{"brokers_fee", -6000.0},
		[2]any{"brokers_fee", -3000.0},
		[2]any{"brokers_fee", -1200.0},
	)
}

// TestPortfolioForecastRangeAndTargetControl covers the re-list forecast's
// view-level control (docs/spec/v2.md §4.7): the target net margin is
// URL-carried and defaults to 0% (Target = Break-even), and each forecast
// column shows the conservative high end as the headline with the
// confidently-allocated low end beside it.
func TestPortfolioForecastRangeAndTargetControl(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedPortfolioForecastFixtures(t, sqlDB)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	// Default: 0% net target. Break-even high headlines 115.42 with the
	// allocated low 115.08; at a 0% target, Target equals Break-even.
	_, body := getBody(t, srv.URL+"/portfolio")
	if !strings.Contains(body, `name="targetmargin"`) {
		t.Errorf("GET /portfolio missing target-margin control; body:\n%s", body)
	}
	if !strings.Contains(body, `value="0"`) {
		t.Errorf("GET /portfolio target-margin control should default to 0")
	}
	for _, want := range []string{"115.42 (low 115.08)", "230.84 (low 230.17)"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing forecast range %q", want)
		}
	}

	// A 5% target raises Target above break-even (121.89/122.25 for A) and
	// round-trips the control's value.
	_, targeted := getBody(t, srv.URL+"/portfolio?targetmargin=5")
	if !strings.Contains(targeted, `value="5"`) {
		t.Errorf("GET /portfolio?targetmargin=5 control did not round-trip 5")
	}
	if !strings.Contains(targeted, "122.25 (low 121.89)") {
		t.Errorf("GET /portfolio?targetmargin=5 body missing the 5%% target range; body:\n%s", targeted)
	}
}

// TestHeaderNavLinksOpportunitiesAndPortfolio asserts the header nav links
// the two views to each other, from both pages.
func TestHeaderNavLinksOpportunitiesAndPortfolio(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedPortfolioFixtures(t, sqlDB)

	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	_, portfolioBody := getBody(t, srv.URL+"/portfolio")
	if !strings.Contains(portfolioBody, `href="/"`) || !strings.Contains(portfolioBody, "Opportunities") {
		t.Errorf("portfolio header missing Opportunities link; body:\n%s", portfolioBody)
	}

	_, indexBody := getBody(t, srv.URL+"/")
	if !strings.Contains(indexBody, `href="/portfolio"`) {
		t.Errorf("opportunities header missing Portfolio link; body:\n%s", indexBody)
	}
}

// seedPortfolioRelistFixtures seeds one resting sell order worth moving: the
// character holds the book's best sell at 100, the next real sell among
// other traders is 120, so raising nets 16,100 ISK after the in-place modify
// fee (docs/spec/v2.md §4.8). A buy order and a below-floor move are seeded
// too, to show they are excluded.
func seedPortfolioRelistFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	dbtest.SeedToken(t, db, portfolioCharacterID, testAuthConfig().TokenKey, "refresh-token")
	dbtest.SeedSkills(t, db, portfolioCharacterID, 0, 0) // R_b 3%, R_t 7.5%

	day1 := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	// Worth moving. Including the character's own order in the public book
	// must not collapse the best sell onto it.
	dbtest.SeedItem(t, db, 400, "Relist Ore")
	seedPortfolioOrder(t, db, 4001, day1, 400, false, 100, 1000, 1000, "")
	dbtest.SeedOrder(t, db, 4001, 400, false, 100)
	dbtest.SeedOrder(t, db, 9500, 400, false, 120)
	dbtest.SeedOrder(t, db, 9501, 400, false, 125)

	// A buy order is never a raise-only gain.
	dbtest.SeedItem(t, db, 401, "Bid Ore")
	seedPortfolioOrder(t, db, 4002, day1, 401, true, 90, 1000, 1000, "")

	// A one-unit move is swamped by the 100 ISK modify-fee floor.
	dbtest.SeedItem(t, db, 402, "Floor Ore")
	seedPortfolioOrder(t, db, 4003, day1, 402, false, 119.99, 1, 1, "")
	dbtest.SeedOrder(t, db, 9503, 402, false, 120)
}

// TestPortfolioOrdersWorthMovingGroup covers the re-list-gain group
// (docs/spec/v2.md §4.8, §7): the Portfolio surfaces one row per order whose
// raise-only move nets positive, with item/location, current -> suggested
// price, and the net gain.
func TestPortfolioOrdersWorthMovingGroup(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	seedPortfolioRelistFixtures(t, sqlDB)
	body := renderPortfolio(t, sqlDB)

	if !strings.Contains(body, "Orders worth moving") {
		t.Fatalf("GET /portfolio body missing group %q", "Orders worth moving")
	}
	for _, want := range []string{"Relist Ore", "100.00", "120.00", "16,100 ISK"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /portfolio body missing relist-gain value %q", want)
		}
	}
	for _, unwanted := range []string{"Bid Ore", "Floor Ore"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("GET /portfolio body surfaced excluded item %q", unwanted)
		}
	}

	// The group sits between Below target and Transfers, per the spec's
	// action ordering.
	below := strings.Index(body, "Below target")
	relist := strings.Index(body, "Orders worth moving")
	transfers := strings.Index(body, "Transfers")
	if !(below < relist && relist < transfers) {
		t.Errorf("group order = Below %d, Orders %d, Transfers %d; want Below < Orders < Transfers", below, relist, transfers)
	}
}
