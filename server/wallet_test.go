package server_test

import (
	"database/sql"
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

func TestWalletPollerWithoutStoredTokenIsNoOp(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := server.New(gateway, sqlDB, testAuthConfig())

	if err := srv.NewWalletPoller(server.WalletSyncInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want nil with no stored token", err)
	}
	if got := gateway.calls.Load(); got != 0 {
		t.Errorf("RefreshToken calls = %d, want 0 with no stored token", got)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_transaction`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("wallet_transaction rows = %d, want 0 with no stored token", count)
	}
}

func TestWalletPollerRefreshFailureReturnsError(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{RefreshTokenErr: errors.New("refresh token revoked")}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewWalletPoller(server.WalletSyncInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want refresh failure")
	}
}

// TestWalletPollerInsufficientScopeLatchesReauthenticationBanner covers
// AC1's "one-time re-consent path": a stored refresh token minted before
// the v2 scope expansion refreshes fine but ESI 403s the wallet call
// itself, and that must surface the same re-authentication banner a
// refresh failure does (its link re-requests the full v2 scope set).
func TestWalletPollerInsufficientScopeLatchesReauthenticationBanner(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{
		RefreshTokenToken:          esi.Token{AccessToken: "access", CharacterID: 1},
		FetchWalletTransactionsErr: &esi.HTTPError{StatusCode: http.StatusForbidden, Status: "403 Forbidden"},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewWalletPoller(server.WalletSyncInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want the 403 to surface as an error")
	}

	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / after insufficient-scope wallet fetch = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
}

// TestWalletPollerBackfillsFullHistoryThenPollsIncrementally exercises the
// core ledger-foundation contract: a fresh database backfills every seeded
// transaction by walking from_id backward (dropping the inclusive
// boundary duplicate along the way), marks the stream backfilled, and a
// later poll that only adds a newer transaction does not re-walk history.
func TestWalletPollerBackfillsFullHistoryThenPollsIncrementally(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		WalletTransactions: []esi.WalletTransaction{
			tx(300, 34, 10, 5.0, false),
			tx(200, 34, 10, 5.0, false),
			tx(100, 34, 10, 5.0, true),
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	poller := srv.NewWalletPoller(server.WalletSyncInterval)

	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}

	for _, id := range []int64{100, 200, 300} {
		var count int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_transaction WHERE transaction_id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("wallet_transaction %d count = %d, want 1 after backfill", id, count)
		}
	}

	var (
		oldestID, newestID sql.NullInt64
		backfilled         int
	)
	if err := sqlDB.QueryRow(
		`SELECT oldest_id, newest_id, backfilled FROM ledger_sync WHERE stream = 'wallet_transaction'`,
	).Scan(&oldestID, &newestID, &backfilled); err != nil {
		t.Fatalf("reading ledger_sync: %v", err)
	}
	if !oldestID.Valid || oldestID.Int64 != 100 {
		t.Errorf("ledger_sync.oldest_id = %v, want 100", oldestID)
	}
	if !newestID.Valid || newestID.Int64 != 300 {
		t.Errorf("ledger_sync.newest_id = %v, want 300", newestID)
	}
	if backfilled != 1 {
		t.Errorf("ledger_sync.backfilled = %d, want 1 after reaching the end of history", backfilled)
	}

	// A later poll adds a newer transaction. Because the stream is already
	// backfilled, the poller must fetch only the current page, not re-walk
	// the already-synced history: seed the fake so a from_id call (the
	// backward walk) would return nothing usable, proving it isn't relied on.
	fake.WalletTransactions = []esi.WalletTransaction{
		tx(400, 34, 10, 5.0, false),
		tx(300, 34, 10, 5.0, false),
	}
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_transaction WHERE transaction_id = 400`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("wallet_transaction 400 count = %d, want 1 after incremental poll", count)
	}
	// The oldest transaction from the first backfill must still be intact;
	// an incremental poll never deletes prior history.
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_transaction WHERE transaction_id = 100`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("wallet_transaction 100 count = %d, want 1 (retained from first backfill)", count)
	}
}

// TestWalletPollerLinksTransactionToJournalViaContextID is the ticket's
// load-bearing proof: a seeded transaction and its journal entry link via
// transaction_id == context_id (with context_id_type =
// market_transaction_id), and the transaction's own journal_ref_id --
// deliberately seeded wrong -- must play no part in it.
func TestWalletPollerLinksTransactionToJournalViaContextID(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	const (
		transactionID   = int64(555)
		wrongJournalRef = int64(999) // deliberately not the journal id below
		journalID       = int64(42)
	)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		WalletTransactions: []esi.WalletTransaction{
			{TransactionID: transactionID, TypeID: 34, Quantity: 10, UnitPrice: 5.0, IsBuy: false, JournalRefID: wrongJournalRef},
		},
		WalletJournal: []esi.WalletJournalEntry{
			{ID: journalID, RefType: "market_transaction", Amount: 45.0, ContextID: transactionID, ContextIDType: "market_transaction_id", Description: "sale"},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewWalletPoller(server.WalletSyncInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	var linkedJournalID int64
	err := sqlDB.QueryRow(`
		SELECT wallet_journal.id
		FROM wallet_transaction
		JOIN wallet_journal
		  ON wallet_journal.context_id = wallet_transaction.transaction_id
		 AND wallet_journal.context_id_type = 'market_transaction_id'
		WHERE wallet_transaction.transaction_id = ?`,
		transactionID,
	).Scan(&linkedJournalID)
	if err != nil {
		t.Fatalf("joining transaction to journal via context_id: %v", err)
	}
	if linkedJournalID != journalID {
		t.Fatalf("linked journal id = %d, want %d", linkedJournalID, journalID)
	}

	var storedJournalRef int64
	if err := sqlDB.QueryRow(`SELECT journal_ref_id FROM wallet_transaction WHERE transaction_id = ?`, transactionID).Scan(&storedJournalRef); err != nil {
		t.Fatal(err)
	}
	if storedJournalRef == linkedJournalID {
		t.Fatal("journal_ref_id coincidentally matches the linked journal id; the fixture must keep them distinct so the test proves journal_ref_id is unused")
	}
	if storedJournalRef != wrongJournalRef {
		t.Fatalf("stored journal_ref_id = %d, want the unreliable seeded value %d preserved as raw data", storedJournalRef, wrongJournalRef)
	}
}

func TestWalletPollerUpsertIsIdempotent(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	fake := &esi.Fake{
		RefreshTokenToken:  esi.Token{AccessToken: "access", CharacterID: 1},
		WalletTransactions: []esi.WalletTransaction{tx(100, 34, 10, 5.0, true)},
		WalletJournal: []esi.WalletJournalEntry{
			{ID: 1, RefType: "market_transaction", ContextID: 100, ContextIDType: "market_transaction_id"},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	poller := srv.NewWalletPoller(server.WalletSyncInterval)

	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}

	var txCount, journalCount int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_transaction`).Scan(&txCount); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM wallet_journal`).Scan(&journalCount); err != nil {
		t.Fatal(err)
	}
	if txCount != 1 {
		t.Errorf("wallet_transaction rows = %d, want 1 after polling the same data twice", txCount)
	}
	if journalCount != 1 {
		t.Errorf("wallet_journal rows = %d, want 1 after polling the same data twice", journalCount)
	}
}

// tx builds an esi.WalletTransaction fixture with a fixed date, so tests
// don't need to spell out a timestamp for every row.
func tx(id int64, typeID, quantity int, unitPrice float64, isBuy bool) esi.WalletTransaction {
	return esi.WalletTransaction{
		TransactionID: id,
		Date:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TypeID:        typeID,
		Quantity:      quantity,
		UnitPrice:     unitPrice,
		IsBuy:         isBuy,
	}
}
