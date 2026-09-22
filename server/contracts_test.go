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
	"github.com/mgoodness/eve-trader/ledger"
	"github.com/mgoodness/eve-trader/server"
)

func TestContractPollerWithoutStoredTokenIsNoOp(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := server.New(gateway, sqlDB, testAuthConfig())

	if err := srv.NewContractPoller(server.ContractSyncInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want nil with no stored token", err)
	}
	if got := gateway.calls.Load(); got != 0 {
		t.Errorf("RefreshToken calls = %d, want 0 with no stored token", got)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM contract`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("contract rows = %d, want 0 with no stored token", count)
	}
}

func TestContractPollerRefreshFailureReturnsError(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{RefreshTokenErr: errors.New("refresh token revoked")}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewContractPoller(server.ContractSyncInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want refresh failure")
	}
}

func TestContractPollerInsufficientScopeLatchesReauthenticationBanner(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	fake := &esi.Fake{
		RefreshTokenToken:          esi.Token{AccessToken: "access", CharacterID: 1},
		FetchCharacterContractsErr: &esi.HTTPError{StatusCode: http.StatusForbidden, Status: "403 Forbidden"},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())

	if err := srv.NewContractPoller(server.ContractSyncInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want the 403 to surface as an error")
	}

	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / after insufficient-scope contract fetch = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
}

// TestContractPollerStoresItemExchangeContractsAndItemsThenProducesTransfer
// covers AC2 and AC4 end to end: the poller records the contract and its
// items, and the ledger then derives a transfer disposition from the
// zero-price handoff. It also proves only item-exchange contracts get their
// items fetched.
func TestContractPollerStoresItemExchangeContractsAndItemsThenProducesTransfer(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 7, testAuthConfig().TokenKey, "refresh")

	issued := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 7},
		Contracts: []esi.Contract{
			{ContractID: 5001, IssuerID: 7, AssigneeID: 9, Type: "item_exchange", Status: "finished", Price: 0, DateIssued: issued},
			{ContractID: 5002, IssuerID: 7, Type: "courier", Status: "outstanding", DateIssued: issued},
		},
		ContractItems: map[int64][]esi.ContractItem{
			5001: {{RecordID: 1, TypeID: 34, Quantity: 200, IsIncluded: true}},
			// Seeded so a buggy poller that fetched items for a courier
			// would store them and fail this test.
			5002: {{RecordID: 1, TypeID: 35, Quantity: 999, IsIncluded: true}},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewContractPoller(server.ContractSyncInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	var (
		status string
		price  float64
	)
	if err := sqlDB.QueryRow(`SELECT status, price FROM contract WHERE contract_id = 5001`).Scan(&status, &price); err != nil {
		t.Fatalf("reading stored contract: %v", err)
	}
	if status != "finished" || price != 0 {
		t.Fatalf("contract 5001 status=%q price=%v, want finished/0", status, price)
	}

	var quantity int
	if err := sqlDB.QueryRow(`SELECT quantity FROM contract_item WHERE contract_id = 5001 AND record_id = 1`).Scan(&quantity); err != nil {
		t.Fatalf("reading stored contract item: %v", err)
	}
	if quantity != 200 {
		t.Fatalf("contract_item quantity = %d, want 200", quantity)
	}

	var courierItems int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM contract_item WHERE contract_id = 5002`).Scan(&courierItems); err != nil {
		t.Fatal(err)
	}
	if courierItems != 0 {
		t.Errorf("courier contract_item rows = %d, want 0 (items are only fetched for item_exchange)", courierItems)
	}

	transfers, err := ledger.LoadTransfers(t.Context(), sqlDB, 7)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("LoadTransfers() = %+v, want the stored handoff as a transfer", transfers)
	}
	if transfers[0].TypeID != 34 || transfers[0].Quantity != 200 || transfers[0].Source != ledger.TransferContract {
		t.Fatalf("transfer = %+v, want type 34 qty 200 from contract", transfers[0])
	}
}

func TestContractPollerUpsertIsIdempotent(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 7, testAuthConfig().TokenKey, "refresh")

	issued := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 7},
		Contracts: []esi.Contract{
			{ContractID: 5001, IssuerID: 7, Type: "item_exchange", Status: "outstanding", Price: 5, DateIssued: issued},
		},
		ContractItems: map[int64][]esi.ContractItem{
			5001: {{RecordID: 1, TypeID: 34, Quantity: 200, IsIncluded: true}},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	poller := srv.NewContractPoller(server.ContractSyncInterval)

	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	// A later poll sees the contract changed: the upsert must update in
	// place rather than duplicate.
	fake.Contracts[0].Status = "finished"
	fake.Contracts[0].Price = 0
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}

	var contractCount, itemCount int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM contract`).Scan(&contractCount); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM contract_item`).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if contractCount != 1 {
		t.Errorf("contract rows = %d, want 1 after polling the same contract twice", contractCount)
	}
	if itemCount != 1 {
		t.Errorf("contract_item rows = %d, want 1 after polling the same item twice", itemCount)
	}

	var status string
	if err := sqlDB.QueryRow(`SELECT status FROM contract WHERE contract_id = 5001`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "finished" {
		t.Errorf("contract status = %q, want the updated finished", status)
	}
}

func TestContractPollerRecordsSyncState(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 7, testAuthConfig().TokenKey, "refresh")

	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 7},
		Contracts: []esi.Contract{
			{ContractID: 5002, IssuerID: 7, Type: "courier", Status: "outstanding"},
			{ContractID: 5001, IssuerID: 7, Type: "courier", Status: "outstanding"},
		},
	}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewContractPoller(server.ContractSyncInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	var (
		oldestID, newestID sql.NullInt64
		backfilled         int
	)
	if err := sqlDB.QueryRow(
		`SELECT oldest_id, newest_id, backfilled FROM ledger_sync WHERE stream = 'contract'`,
	).Scan(&oldestID, &newestID, &backfilled); err != nil {
		t.Fatalf("reading ledger_sync: %v", err)
	}
	if !oldestID.Valid || oldestID.Int64 != 5001 {
		t.Errorf("ledger_sync.oldest_id = %v, want 5001", oldestID)
	}
	if !newestID.Valid || newestID.Int64 != 5002 {
		t.Errorf("ledger_sync.newest_id = %v, want 5002", newestID)
	}
	if backfilled != 1 {
		t.Errorf("ledger_sync.backfilled = %d, want 1", backfilled)
	}
}
