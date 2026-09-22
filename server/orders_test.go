package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

func TestCharacterOrderPollerWithoutStoredTokenIsNoOp(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := server.New(gateway, sqlDB, testAuthConfig())

	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want nil with no stored token", err)
	}
	if got := gateway.calls.Load(); got != 0 {
		t.Errorf("RefreshToken calls = %d, want 0 with no stored token", got)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM character_order`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("character_order rows = %d, want 0 with no stored token", count)
	}
}

func TestCharacterOrderPollerRefreshFailureReturnsError(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{RefreshTokenErr: errors.New("refresh token revoked")}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want refresh failure")
	}
}

func TestCharacterOrderPollerInsufficientScopeLatchesReauthenticationBanner(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{
		RefreshTokenToken:       esi.Token{AccessToken: "access", CharacterID: 1},
		FetchCharacterOrdersErr: &esi.HTTPError{StatusCode: http.StatusForbidden, Status: "403 Forbidden"},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want the 403 to surface as an error")
	}

	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / after insufficient-scope order fetch = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
}

// TestCharacterOrderPollerSnapshotsInPlaceModify covers the (order_id,
// issued) key (docs/spec/v2.md §3, §5): a modified order keeps its
// order_id but gets a new issued, and both snapshots must survive.
func TestCharacterOrderPollerSnapshotsInPlaceModify(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	first := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	second := first.Add(30 * time.Minute)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		CharacterOrders: []esi.CharacterOrder{
			{OrderID: 500, TypeID: 34, LocationID: 60004588, IsBuyOrder: true, Price: 5.0, VolumeRemain: 100, VolumeTotal: 100, Issued: first},
			{OrderID: 500, TypeID: 34, LocationID: 60004588, IsBuyOrder: true, Price: 5.5, VolumeRemain: 100, VolumeTotal: 100, Issued: second},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	rows, err := sqlDB.Query(`SELECT issued, price FROM character_order WHERE order_id = 500 ORDER BY issued`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type snapshot struct {
		issued string
		price  float64
	}
	var got []snapshot
	for rows.Next() {
		var s snapshot
		if err := rows.Scan(&s.issued, &s.price); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("character_order rows for order 500 = %d, want 2 snapshots after an in-place modify", len(got))
	}
	if got[0].issued != first.Format(time.RFC3339Nano) || got[0].price != 5.0 {
		t.Errorf("first snapshot = %+v, want issued %s price 5.0", got[0], first.Format(time.RFC3339Nano))
	}
	if got[1].issued != second.Format(time.RFC3339Nano) || got[1].price != 5.5 {
		t.Errorf("second snapshot = %+v, want issued %s price 5.5", got[1], second.Format(time.RFC3339Nano))
	}
}

// TestCharacterOrderPollerReconstructsRelistChain is the ticket's proof
// that the ledger makes re-list chains inferable: a cancelled sell order
// plus a later replacement of the same (type_id, location_id, side) are
// stored as two snapshots and can be walked in issued order. The
// missing-is_buy_order decode itself is covered by
// TestHTTPGatewayFetchCharacterOrderHistoryMissingIsBuyOrderIsSell; here
// the cancelled order is a sell and must come back as is_buy_order = 0.
func TestCharacterOrderPollerReconstructsRelistChain(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	const (
		typeID     = 626
		locationID = 60004588
	)
	cancelled := time.Date(2026, 7, 24, 2, 13, 39, 0, time.UTC)
	replacement := time.Date(2026, 8, 18, 14, 15, 1, 0, time.UTC)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		CharacterOrderHistory: []esi.CharacterOrder{
			{OrderID: 10, TypeID: typeID, LocationID: locationID, IsBuyOrder: false, Price: 12790000, VolumeRemain: 1, VolumeTotal: 1, Issued: cancelled, State: "cancelled"},
			{OrderID: 11, TypeID: typeID, LocationID: locationID, IsBuyOrder: false, Price: 11500000, VolumeRemain: 1, VolumeTotal: 1, Issued: replacement, State: "expired"},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	rows, err := sqlDB.Query(`
		SELECT order_id, issued, state, is_buy_order
		FROM character_order
		WHERE type_id = ? AND location_id = ? AND is_buy_order = 0
		ORDER BY issued`, typeID, locationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var chain []struct {
		orderID int64
		state   string
	}
	for rows.Next() {
		var (
			link struct {
				orderID int64
				state   string
			}
			issued     string
			isBuyOrder int
		)
		if err := rows.Scan(&link.orderID, &issued, &link.state, &isBuyOrder); err != nil {
			t.Fatal(err)
		}
		if isBuyOrder != 0 {
			t.Fatalf("order %d stored is_buy_order = %d, want 0 (sell)", link.orderID, isBuyOrder)
		}
		chain = append(chain, link)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(chain) != 2 {
		t.Fatalf("re-list chain length = %d, want 2 (cancelled order plus its replacement)", len(chain))
	}
	if chain[0].orderID != 10 || chain[0].state != "cancelled" {
		t.Errorf("chain[0] = order %d state %q, want order 10 cancelled", chain[0].orderID, chain[0].state)
	}
	if chain[1].orderID != 11 || chain[1].state != "expired" {
		t.Errorf("chain[1] = order %d state %q, want order 11 expired (the later replacement)", chain[1].orderID, chain[1].state)
	}
}

func TestCharacterOrderPollerStoresOpenOrdersWithEmptyState(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		CharacterOrders: []esi.CharacterOrder{
			{OrderID: 1, TypeID: 34, LocationID: 60004588, IsBuyOrder: true, Price: 5.0, Issued: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		},
		CharacterOrderHistory: []esi.CharacterOrder{
			{OrderID: 2, TypeID: 34, LocationID: 60004588, IsBuyOrder: false, Price: 6.0, Issued: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), State: "cancelled"},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	var openState, historyState string
	if err := sqlDB.QueryRow(`SELECT state FROM character_order WHERE order_id = 1`).Scan(&openState); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(`SELECT state FROM character_order WHERE order_id = 2`).Scan(&historyState); err != nil {
		t.Fatal(err)
	}
	if openState != "" {
		t.Errorf("open order state = %q, want empty", openState)
	}
	if historyState != "cancelled" {
		t.Errorf("history order state = %q, want cancelled", historyState)
	}
}

func TestCharacterOrderPollerUpsertIsIdempotent(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	order := esi.CharacterOrder{
		OrderID: 1, TypeID: 34, LocationID: 60004588, IsBuyOrder: true, Price: 5.0,
		Issued: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		CharacterOrders:   []esi.CharacterOrder{order},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	poller := srv.NewCharacterOrderPoller(server.CharacterOrdersInterval)

	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM character_order`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("character_order rows = %d, want 1 after polling the same snapshot twice", count)
	}
}
