// Package ledger reads portfolio facts from eve-trader's append-only local
// ledger of raw ESI records (docs/spec/v2.md §5). Positions and P/L are
// derived on read, never persisted, so this package holds only the
// derivation: for now, the Transfer disposition produced by item-exchange
// contracts and user-entered manual transfers (docs/spec/v2.md §4.5).
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// TransferSource names where a Transfer came from: a contract ESI can see,
// or a manual record for the direct-trade blind spot.
type TransferSource string

const (
	// TransferContract is a transfer detected from an item-exchange
	// contract's items.
	TransferContract TransferSource = "contract"
	// TransferManual is a transfer the user recorded by hand.
	TransferManual TransferSource = "manual"
)

// Transfer is a disposition: goods that left the character without a market
// sale (docs/spec/v2.md §4.5). Quantity is in whole units and may be less
// than the whole position. Price is the total ISK value of the transfer:
// for a contract, ESI's whole-contract price (which may cover several of
// the contract's items), or the value the user entered for a manual
// transfer. Zero means value the transfer at the position's average cost. A
// contract-sourced transfer carries no location ESI documents, so its
// LocationID is the contract's start location when ESI supplies one, else
// zero.
type Transfer struct {
	TypeID       int
	LocationID   int64
	Quantity     int
	Date         time.Time
	Price        float64
	Counterparty string
	Note         string
	Source       TransferSource
}

// ManualTransfer is a user-entered handoff invisible to ESI -- an in-game
// direct trade -- recorded at partial-quantity grain. Price is the total
// value of the entered quantity, not a per-unit price.
type ManualTransfer struct {
	TypeID       int
	LocationID   int64
	Quantity     int
	Date         time.Time
	Price        float64
	Counterparty string
	Note         string
}

// LoadTransfers returns every Transfer disposition in the ledger: the
// goods-moving items of the character's item-exchange contracts, plus every
// manual transfer. characterID is the tracked character, used to decide
// which side of a contract gave up the goods. The result is ordered by
// date, oldest first, so downstream cost-basis math can replay it.
func LoadTransfers(ctx context.Context, db *sql.DB, characterID int) ([]Transfer, error) {
	contractTransfers, err := loadContractTransfers(ctx, db, characterID)
	if err != nil {
		return nil, err
	}
	manualTransfers, err := loadManualTransfers(ctx, db)
	if err != nil {
		return nil, err
	}

	out := append(contractTransfers, manualTransfers...)
	sortTransfersByDate(out)
	return out, nil
}

// loadContractTransfers reads every item of a finished item_exchange
// contract in which the character gave up the goods. Contracts that are
// still outstanding or were cancelled/rejected/failed moved no goods, so
// they are not transfers (their items are held in escrow or came back).
//
// ESI prices the contract as a whole, not per item, so when a contract moves
// several item stacks its price is split evenly across the stacks that left.
// That keeps the total transfer value equal to the contract price instead of
// counting it once per stack; a single-item handoff is unchanged.
func loadContractTransfers(ctx context.Context, db *sql.DB, characterID int) ([]Transfer, error) {
	type contractTransferRow struct {
		contractID int64
		typeID     int
		locationID int64
		quantity   int
		dateIssued string
		price      float64
		issuerID   int64
		acceptorID int64
		isIncluded bool
	}

	rows, err := db.QueryContext(ctx, `
		SELECT ci.type_id, COALESCE(c.start_location_id, 0), ci.quantity, c.date_issued, c.price,
		       c.issuer_id, c.acceptor_id, ci.is_included, c.contract_id
		FROM contract c
		JOIN contract_item ci ON ci.contract_id = c.contract_id
		WHERE c.type = 'item_exchange'
		  AND c.status = 'finished'
		ORDER BY c.contract_id, ci.record_id`)
	if err != nil {
		return nil, fmt.Errorf("querying contract transfers: %w", err)
	}
	defer rows.Close()

	var records []contractTransferRow
	for rows.Next() {
		var r contractTransferRow
		if err := rows.Scan(&r.typeID, &r.locationID, &r.quantity, &r.dateIssued, &r.price,
			&r.issuerID, &r.acceptorID, &r.isIncluded, &r.contractID); err != nil {
			return nil, fmt.Errorf("scanning contract transfer: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading contract transfers: %w", err)
	}

	leavingItems := map[int64]int{}
	for _, r := range records {
		if goodsLeft(r.issuerID, r.acceptorID, r.isIncluded, characterID) {
			leavingItems[r.contractID]++
		}
	}

	var out []Transfer
	for _, r := range records {
		if !goodsLeft(r.issuerID, r.acceptorID, r.isIncluded, characterID) {
			continue
		}
		date, err := time.Parse(time.RFC3339Nano, r.dateIssued)
		if err != nil {
			return nil, fmt.Errorf("parsing contract %d date_issued: %w", r.contractID, err)
		}
		var value float64
		if r.price > 0 && leavingItems[r.contractID] > 0 {
			value = r.price / float64(leavingItems[r.contractID])
		}
		out = append(out, Transfer{
			TypeID:     r.typeID,
			LocationID: r.locationID,
			Quantity:   r.quantity,
			Date:       date,
			Price:      value,
			Source:     TransferContract,
		})
	}
	return out, nil
}

// goodsLeft reports whether a contract item moved goods off characterID.
// As the issuer, the items the character submitted (is_included) leave it.
// As the acceptor, the items the issuer asked for (is_included false) leave
// it; an item the character merely received does not leave.
func goodsLeft(issuerID, acceptorID int64, isIncluded bool, characterID int) bool {
	switch {
	case issuerID == int64(characterID):
		return isIncluded
	case acceptorID == int64(characterID):
		return !isIncluded
	default:
		return false
	}
}

// loadManualTransfers reads every user-entered transfer.
func loadManualTransfers(ctx context.Context, db *sql.DB) ([]Transfer, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT type_id, location_id, quantity, date, price, counterparty, note
		FROM manual_transfer`)
	if err != nil {
		return nil, fmt.Errorf("querying manual transfers: %w", err)
	}
	defer rows.Close()

	var out []Transfer
	for rows.Next() {
		var (
			tr           Transfer
			date         string
			counterparty sql.NullString
			note         sql.NullString
		)
		if err := rows.Scan(&tr.TypeID, &tr.LocationID, &tr.Quantity, &date, &tr.Price, &counterparty, &note); err != nil {
			return nil, fmt.Errorf("scanning manual transfer: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, date)
		if err != nil {
			return nil, fmt.Errorf("parsing manual transfer date %s: %w", date, err)
		}
		tr.Date = parsed
		tr.Counterparty = counterparty.String
		tr.Note = note.String
		tr.Source = TransferManual
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading manual transfers: %w", err)
	}
	return out, nil
}

// RecordManualTransfer stores a user-entered transfer and returns its new
// id. A zero Date defaults to now. Quantity must be positive: the row is
// one partial quantity of a position, never a no-op.
func RecordManualTransfer(ctx context.Context, db *sql.DB, mt ManualTransfer) (int64, error) {
	if mt.Quantity <= 0 {
		return 0, fmt.Errorf("manual transfer quantity %d must be positive", mt.Quantity)
	}
	if mt.TypeID <= 0 {
		return 0, fmt.Errorf("manual transfer type_id %d must be positive", mt.TypeID)
	}
	if mt.Date.IsZero() {
		mt.Date = time.Now().UTC()
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO manual_transfer (type_id, location_id, quantity, date, price, counterparty, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		mt.TypeID, mt.LocationID, mt.Quantity, mt.Date.UTC().Format(time.RFC3339Nano),
		mt.Price, nullable(mt.Counterparty), nullable(mt.Note), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("recording manual transfer: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading manual transfer id: %w", err)
	}
	return id, nil
}

// sortTransfersByDate sorts transfers oldest first, breaking ties by source
// then type so the order is stable across repeated reads.
func sortTransfersByDate(transfers []Transfer) {
	sort.SliceStable(transfers, func(i, j int) bool {
		a, b := transfers[i], transfers[j]
		if !a.Date.Equal(b.Date) {
			return a.Date.Before(b.Date)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.TypeID < b.TypeID
	})
}

// nullable maps an empty string to SQL NULL so optional manual-transfer
// fields stay genuinely unset.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
