// contracts.go implements the contracts-ledger poller (docs/spec/v2.md §3,
// §5): it fetches the character's contracts through ESIGateway, stores
// every contract and the items of each item-exchange contract, and records
// progress in ledger_sync. The stored item-exchange items are what the
// ledger reads to derive Transfer dispositions (docs/spec/v2.md §4.5).
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

// ContractSyncInterval is how often ContractPoller refreshes the contracts
// ledger, matching docs/spec/v2.md §5's ~15 min contracts cadence.
const ContractSyncInterval = 15 * time.Minute

// contractStream names the contracts stream in ledger_sync.
const contractStream ledgerStream = "contract"

// ContractPoller keeps contract and contract_item current. It shares the
// server's token-refresh/re-authentication path exactly like WalletPoller.
type ContractPoller struct {
	server   *Server
	interval time.Duration
}

// NewContractPoller creates a background contracts-ledger poller.
func (s *Server) NewContractPoller(interval time.Duration) *ContractPoller {
	if interval <= 0 {
		interval = ContractSyncInterval
	}
	return &ContractPoller{server: s, interval: interval}
}

// Poll refreshes the stored token and syncs the contracts ledger. No stored
// token means there is nothing to poll yet (the first-boot state): that is
// a no-op, not a polling failure, exactly like WalletPoller.
func (p *ContractPoller) Poll(ctx context.Context) error {
	token, ok, err := p.server.refreshAuthentication(ctx, true)
	if errors.Is(err, errNoStoredToken) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	var characterID int
	if err := p.server.db.QueryRowContext(ctx, `SELECT character_id FROM esi_token LIMIT 1`).Scan(&characterID); err != nil {
		return fmt.Errorf("loading stored character ID: %w", err)
	}

	if err := p.syncContracts(ctx, characterID, token.AccessToken); err != nil {
		latchIfInsufficientScope(p.server, err)
		return fmt.Errorf("syncing contracts: %w", err)
	}
	return nil
}

// Run performs an immediate sync and then syncs on the configured cadence.
func (p *ContractPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling contracts ledger", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling contracts ledger", "err", err)
			}
		}
	}
}

// syncContracts fetches the character's full retained contract window,
// upserts every contract, fetches and upserts the items of each
// item-exchange contract, then records the stream's ID bounds.
func (p *ContractPoller) syncContracts(ctx context.Context, characterID int, accessToken string) error {
	contracts, err := p.server.gateway.FetchCharacterContracts(ctx, characterID, accessToken)
	if err != nil {
		return fmt.Errorf("fetching contracts: %w", err)
	}
	if err := p.upsertContracts(ctx, contracts); err != nil {
		return err
	}

	for _, c := range contracts {
		if c.Type != "item_exchange" {
			continue
		}
		items, err := p.server.gateway.FetchContractItems(ctx, characterID, accessToken, c.ContractID)
		if err != nil {
			return fmt.Errorf("fetching items for contract %d: %w", c.ContractID, err)
		}
		if err := p.upsertContractItems(ctx, c.ContractID, items); err != nil {
			return err
		}
	}

	state, err := p.server.loadSyncState(ctx, contractStream)
	if err != nil {
		return err
	}
	ids := make([]int64, len(contracts))
	for i, c := range contracts {
		ids[i] = c.ContractID
	}
	state.observeIDs(ids)
	state.backfilled = true
	return p.server.saveSyncState(ctx, contractStream, state)
}

// upsertContracts writes contracts in one transaction, replacing any row
// already stored under the same contract_id.
func (p *ContractPoller) upsertContracts(ctx context.Context, contracts []esi.Contract) error {
	if len(contracts) == 0 {
		return nil
	}
	tx, err := p.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting contract upsert: %w", err)
	}
	defer tx.Rollback()
	for _, c := range contracts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contract (
				contract_id, issuer_id, issuer_corporation_id, assignee_id, acceptor_id,
				type, status, price, for_corporation, date_issued, date_expired, date_completed,
				start_location_id, end_location_id, title, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(contract_id) DO UPDATE SET
			  issuer_id = excluded.issuer_id, issuer_corporation_id = excluded.issuer_corporation_id,
			  assignee_id = excluded.assignee_id, acceptor_id = excluded.acceptor_id,
			  type = excluded.type, status = excluded.status, price = excluded.price,
			  for_corporation = excluded.for_corporation, date_issued = excluded.date_issued,
			  date_expired = excluded.date_expired, date_completed = excluded.date_completed,
			  start_location_id = excluded.start_location_id, end_location_id = excluded.end_location_id,
			  title = excluded.title, updated_at = excluded.updated_at`,
			c.ContractID, c.IssuerID, c.IssuerCorporationID, c.AssigneeID, c.AcceptorID,
			c.Type, c.Status, c.Price, boolToInt(c.ForCorporation),
			c.DateIssued.UTC().Format(time.RFC3339Nano), c.DateExpired.UTC().Format(time.RFC3339Nano),
			nullTime(c.DateCompleted), nullInt64(c.StartLocationID), nullInt64(c.EndLocationID),
			nullString(c.Title), nowUTC(),
		); err != nil {
			return fmt.Errorf("upserting contract %d: %w", c.ContractID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing contract upsert: %w", err)
	}
	return nil
}

// upsertContractItems writes one contract's items in one transaction,
// replacing any row already stored under the same (contract_id, record_id).
func (p *ContractPoller) upsertContractItems(ctx context.Context, contractID int64, items []esi.ContractItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := p.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting contract item upsert: %w", err)
	}
	defer tx.Rollback()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contract_item (contract_id, record_id, type_id, quantity, is_singleton, is_included)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(contract_id, record_id) DO UPDATE SET
			  type_id = excluded.type_id, quantity = excluded.quantity,
			  is_singleton = excluded.is_singleton, is_included = excluded.is_included`,
			contractID, item.RecordID, item.TypeID, item.Quantity,
			boolToInt(item.IsSingleton), boolToInt(item.IsIncluded),
		); err != nil {
			return fmt.Errorf("upserting contract %d item %d: %w", contractID, item.RecordID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing contract item upsert: %w", err)
	}
	return nil
}

// boolToInt maps a bool to SQLite's 0/1 representation.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullTime maps a zero time to SQL NULL.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// nullInt64 maps zero to SQL NULL.
func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// nullString maps an empty string to SQL NULL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
