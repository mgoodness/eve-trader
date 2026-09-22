package ledger_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ledger"
)

// seedContract inserts a finished contract row with the fields transfer
// detection cares about; other columns get benign zero values.
func seedContract(t *testing.T, sqlDB *sql.DB, contractID, issuerID, acceptorID int64, contractType string, price float64, issued time.Time) {
	t.Helper()
	seedContractStatus(t, sqlDB, contractID, issuerID, acceptorID, contractType, "finished", price, issued)
}

// seedContractStatus inserts a contract row with an explicit status.
func seedContractStatus(t *testing.T, sqlDB *sql.DB, contractID, issuerID, acceptorID int64, contractType, status string, price float64, issued time.Time) {
	t.Helper()
	if _, err := sqlDB.Exec(`
		INSERT INTO contract (
			contract_id, issuer_id, issuer_corporation_id, assignee_id, acceptor_id,
			type, status, price, for_corporation, date_issued, date_expired, updated_at
		) VALUES (?, ?, 0, 0, ?, ?, ?, ?, 0, ?, ?, '2024-01-01T00:00:00Z')`,
		contractID, issuerID, acceptorID, contractType, status, price,
		issued.UTC().Format(time.RFC3339Nano), issued.AddDate(0, 0, 7).UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("seeding contract %d: %v", contractID, err)
	}
}

func seedContractItem(t *testing.T, sqlDB *sql.DB, contractID, recordID int64, typeID, quantity int, isIncluded bool) {
	t.Helper()
	if _, err := sqlDB.Exec(`
		INSERT INTO contract_item (contract_id, record_id, type_id, quantity, is_singleton, is_included)
		VALUES (?, ?, ?, ?, 0, ?)`,
		contractID, recordID, typeID, quantity, isIncluded,
	); err != nil {
		t.Fatalf("seeding contract_item %d/%d: %v", contractID, recordID, err)
	}
}

// TestLoadTransfersProducesDispositionsFromContractAndManual covers the
// ticket's acceptance test: a zero-price item-exchange contract handoff and
// a manual transfer each become a Transfer disposition.
func TestLoadTransfersProducesDispositionsFromContractAndManual(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	const characterID = 123

	issued := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	seedContract(t, sqlDB, 5001, characterID, 900, "item_exchange", 0, issued)
	seedContractItem(t, sqlDB, 5001, 1, 34, 200, true)

	if _, err := sqlDB.Exec(`
		INSERT INTO manual_transfer (type_id, location_id, quantity, date, price, counterparty, note, created_at)
		VALUES (34, 60004588, 30, '2024-02-02T00:00:00Z', 0, 'an alt', 'direct trade', '2024-02-02T00:00:00Z')`,
	); err != nil {
		t.Fatal(err)
	}

	got, err := ledger.LoadTransfers(t.Context(), sqlDB, characterID)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadTransfers() = %+v, want the contract and manual transfers", got)
	}

	bySource := map[ledger.TransferSource]ledger.Transfer{}
	for _, tr := range got {
		bySource[tr.Source] = tr
	}

	contract, ok := bySource[ledger.TransferContract]
	if !ok {
		t.Fatal("no contract-sourced transfer")
	}
	if contract.TypeID != 34 || contract.Quantity != 200 || contract.Price != 0 {
		t.Fatalf("contract transfer = %+v, want type 34 qty 200 at zero price", contract)
	}

	manual, ok := bySource[ledger.TransferManual]
	if !ok {
		t.Fatal("no manual transfer")
	}
	if manual.TypeID != 34 || manual.Quantity != 30 || manual.LocationID != 60004588 {
		t.Fatalf("manual transfer = %+v, want type 34 qty 30 at the seeded location", manual)
	}
	if manual.Counterparty != "an alt" || manual.Note != "direct trade" {
		t.Fatalf("manual transfer = %+v, want counterparty and note preserved", manual)
	}
}

// TestLoadTransfersOnlyCountsGoodsLeavingTheCharacter pins the contract
// direction rule: goods the character issued leave; goods the character
// received do not.
func TestLoadTransfersOnlyCountsGoodsLeavingTheCharacter(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	const characterID = 123
	issued := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)

	// Issued by the character, item submitted: leaves.
	seedContract(t, sqlDB, 1, characterID, 0, "item_exchange", 0, issued)
	seedContractItem(t, sqlDB, 1, 1, 34, 10, true)
	// Issued by the character, item requested: enters.
	seedContract(t, sqlDB, 2, characterID, 0, "item_exchange", 0, issued)
	seedContractItem(t, sqlDB, 2, 1, 35, 20, false)
	// Issued by another, accepted by the character, item submitted: enters.
	seedContract(t, sqlDB, 3, 900, characterID, "item_exchange", 0, issued)
	seedContractItem(t, sqlDB, 3, 1, 36, 30, true)
	// Non-item-exchange contract: never a transfer.
	seedContract(t, sqlDB, 4, characterID, 0, "courier", 0, issued)
	seedContractItem(t, sqlDB, 4, 1, 37, 40, true)

	got, err := ledger.LoadTransfers(t.Context(), sqlDB, characterID)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadTransfers() = %+v, want only the issued item_exchange item", got)
	}
	if got[0].TypeID != 34 || got[0].Quantity != 10 {
		t.Fatalf("transfer = %+v, want type 34 qty 10", got[0])
	}
}

func TestLoadTransfersIgnoresUnfinishedContracts(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	const characterID = 123
	issued := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)

	// Only a fully finished contract counts; outstanding, cancelled,
	// rejected, and one-sided "finished_issuer" states move no goods the
	// character has permanently given up.
	seedContractStatus(t, sqlDB, 1, characterID, 0, "item_exchange", "outstanding", 0, issued)
	seedContractItem(t, sqlDB, 1, 1, 34, 10, true)
	seedContractStatus(t, sqlDB, 2, characterID, 0, "item_exchange", "cancelled", 0, issued)
	seedContractItem(t, sqlDB, 2, 1, 35, 20, true)
	seedContractStatus(t, sqlDB, 3, characterID, 0, "item_exchange", "finished_issuer", 0, issued)
	seedContractItem(t, sqlDB, 3, 1, 36, 30, true)
	seedContractStatus(t, sqlDB, 4, characterID, 0, "item_exchange", "rejected", 0, issued)
	seedContractItem(t, sqlDB, 4, 1, 37, 40, true)
	seedContractStatus(t, sqlDB, 5, characterID, 0, "item_exchange", "finished", 0, issued)
	seedContractItem(t, sqlDB, 5, 1, 38, 50, true)

	got, err := ledger.LoadTransfers(t.Context(), sqlDB, characterID)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadTransfers() = %+v, want only the finished contract's items", got)
	}
	if got[0].TypeID != 38 || got[0].Quantity != 50 {
		t.Fatalf("transfer = %+v, want type 38 qty 50", got[0])
	}
}

func TestRecordManualTransferStoresPartialQuantity(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)

	id, err := ledger.RecordManualTransfer(t.Context(), sqlDB, ledger.ManualTransfer{
		TypeID:     34,
		LocationID: 60004588,
		Quantity:   25,
		Date:       time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		Price:      7.5,
	})
	if err != nil {
		t.Fatalf("RecordManualTransfer() error = %v", err)
	}
	if id == 0 {
		t.Fatal("RecordManualTransfer() returned id 0")
	}

	got, err := ledger.LoadTransfers(t.Context(), sqlDB, 123)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadTransfers() = %+v, want the recorded manual transfer", got)
	}
	if got[0].TypeID != 34 || got[0].LocationID != 60004588 || got[0].Quantity != 25 || got[0].Price != 7.5 {
		t.Fatalf("transfer = %+v, want the recorded partial quantity", got[0])
	}
	if got[0].Source != ledger.TransferManual {
		t.Fatalf("transfer source = %q, want manual", got[0].Source)
	}
}

func TestRecordManualTransferRejectsNonPositiveQuantity(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)

	for _, quantity := range []int{0, -5} {
		_, err := ledger.RecordManualTransfer(t.Context(), sqlDB, ledger.ManualTransfer{
			TypeID:     34,
			LocationID: 60004588,
			Quantity:   quantity,
			Date:       time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		})
		if err == nil {
			t.Fatalf("RecordManualTransfer() with quantity %d error = nil, want an error", quantity)
		}
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM manual_transfer`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("manual_transfer rows = %d, want 0 after rejected writes", count)
	}
}
