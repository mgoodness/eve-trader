// wallet.go implements the wallet-ledger poller (docs/spec/v2.md §3, §5):
// it fetches the character's wallet transactions and journal through
// ESIGateway, upserts them idempotently into wallet_transaction and
// wallet_journal, and tracks each stream's progress in ledger_sync so a
// fresh database backfills its full retained history exactly once and
// later polls fetch only what changed.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

// WalletSyncInterval is how often WalletPoller refreshes the wallet
// ledger, matching docs/spec/v2.md §5's ~1h wallet cadence.
const WalletSyncInterval = time.Hour

// ledgerStream names one append-only ledger stream tracked by
// ledger_sync.stream. A small closed set, so it is its own type rather
// than a bare string.
type ledgerStream string

const (
	walletTransactionStream ledgerStream = "wallet_transaction"
	walletJournalStream     ledgerStream = "wallet_journal"
)

// WalletPoller keeps wallet_transaction and wallet_journal current. It
// shares the server's token-refresh/re-authentication path exactly like
// SkillPoller.
type WalletPoller struct {
	server   *Server
	interval time.Duration
}

// NewWalletPoller creates a background wallet-ledger poller.
func (s *Server) NewWalletPoller(interval time.Duration) *WalletPoller {
	if interval <= 0 {
		interval = WalletSyncInterval
	}
	return &WalletPoller{server: s, interval: interval}
}

// Poll refreshes the stored token and syncs both wallet streams. No stored
// token means there is nothing to poll yet (the first-boot state): that is
// a no-op, not a polling failure, exactly like SkillPoller.
func (p *WalletPoller) Poll(ctx context.Context) error {
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

	if err := p.syncTransactions(ctx, characterID, token.AccessToken); err != nil {
		p.server.latchIfInsufficientScope(err)
		return fmt.Errorf("syncing wallet transactions: %w", err)
	}
	if err := p.syncJournal(ctx, characterID, token.AccessToken); err != nil {
		p.server.latchIfInsufficientScope(err)
		return fmt.Errorf("syncing wallet journal: %w", err)
	}
	return nil
}

// Run performs an immediate sync and then syncs on the configured cadence.
func (p *WalletPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling wallet ledger", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling wallet ledger", "err", err)
			}
		}
	}
}

// syncTransactions fetches the current page of wallet transactions (always,
// so anything new since the last poll is picked up) and, until this stream
// has been fully backfilled, walks backward with from_id to reach the edge
// of ESI's retained history exactly once.
func (p *WalletPoller) syncTransactions(ctx context.Context, characterID int, accessToken string) error {
	state, err := p.loadSyncState(ctx, walletTransactionStream)
	if err != nil {
		return err
	}

	page, err := p.server.gateway.FetchWalletTransactions(ctx, characterID, accessToken, 0)
	if err != nil {
		return fmt.Errorf("fetching wallet transactions: %w", err)
	}
	if err := p.upsertTransactions(ctx, page); err != nil {
		return err
	}
	state.observeIDs(transactionIDs(page))

	for !state.backfilled {
		if !state.oldestID.Valid {
			// Nothing has ever been seen on this stream (the character has
			// no transactions at all): there is no history to walk back
			// into.
			state.backfilled = true
			break
		}
		cursor := state.oldestID.Int64
		older, err := p.server.gateway.FetchWalletTransactions(ctx, characterID, accessToken, cursor)
		if err != nil {
			return fmt.Errorf("backfilling wallet transactions before %d: %w", cursor, err)
		}
		older = dropTransactionID(older, cursor)
		if len(older) == 0 {
			state.backfilled = true
			break
		}
		if err := p.upsertTransactions(ctx, older); err != nil {
			return err
		}
		if !state.observeIDs(transactionIDs(older)) {
			// The page had entries but none older than the cursor: ESI has
			// nothing further behind it.
			state.backfilled = true
			break
		}
	}

	return p.saveSyncState(ctx, walletTransactionStream, state)
}

// syncJournal fetches the character's whole wallet journal -- ESI always
// returns its full retained window, so there is no separate backward walk
// -- and upserts it idempotently.
func (p *WalletPoller) syncJournal(ctx context.Context, characterID int, accessToken string) error {
	entries, err := p.server.gateway.FetchWalletJournal(ctx, characterID, accessToken)
	if err != nil {
		return fmt.Errorf("fetching wallet journal: %w", err)
	}
	if err := p.upsertJournal(ctx, entries); err != nil {
		return err
	}

	state, err := p.loadSyncState(ctx, walletJournalStream)
	if err != nil {
		return err
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	state.observeIDs(ids)
	state.backfilled = true
	return p.saveSyncState(ctx, walletJournalStream, state)
}

// upsertTransactions writes txs to wallet_transaction in one transaction,
// replacing any row already stored under the same transaction_id.
func (p *WalletPoller) upsertTransactions(ctx context.Context, txs []esi.WalletTransaction) error {
	if len(txs) == 0 {
		return nil
	}
	tx, err := p.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting wallet transaction upsert: %w", err)
	}
	defer tx.Rollback()
	for _, t := range txs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_transaction (transaction_id, date, type_id, quantity, unit_price, is_buy, is_personal, journal_ref_id, location_id, client_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(transaction_id) DO UPDATE SET
			  date = excluded.date, type_id = excluded.type_id, quantity = excluded.quantity,
			  unit_price = excluded.unit_price, is_buy = excluded.is_buy, is_personal = excluded.is_personal,
			  journal_ref_id = excluded.journal_ref_id, location_id = excluded.location_id, client_id = excluded.client_id`,
			t.TransactionID, t.Date.UTC().Format(time.RFC3339Nano), t.TypeID, t.Quantity, t.UnitPrice,
			t.IsBuy, t.IsPersonal, t.JournalRefID, t.LocationID, t.ClientID,
		); err != nil {
			return fmt.Errorf("upserting wallet transaction %d: %w", t.TransactionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing wallet transaction upsert: %w", err)
	}
	return nil
}

// upsertJournal writes entries to wallet_journal in one transaction,
// replacing any row already stored under the same id.
func (p *WalletPoller) upsertJournal(ctx context.Context, entries []esi.WalletJournalEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := p.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting wallet journal upsert: %w", err)
	}
	defer tx.Rollback()
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_journal (id, date, ref_type, amount, balance, context_id, context_id_type, description, first_party_id, second_party_id, reason, tax, tax_receiver_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			  date = excluded.date, ref_type = excluded.ref_type, amount = excluded.amount, balance = excluded.balance,
			  context_id = excluded.context_id, context_id_type = excluded.context_id_type, description = excluded.description,
			  first_party_id = excluded.first_party_id, second_party_id = excluded.second_party_id, reason = excluded.reason,
			  tax = excluded.tax, tax_receiver_id = excluded.tax_receiver_id`,
			e.ID, e.Date.UTC().Format(time.RFC3339Nano), e.RefType, e.Amount, e.Balance, e.ContextID, e.ContextIDType,
			e.Description, e.FirstPartyID, e.SecondPartyID, e.Reason, e.Tax, e.TaxReceiverID,
		); err != nil {
			return fmt.Errorf("upserting wallet journal entry %d: %w", e.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing wallet journal upsert: %w", err)
	}
	return nil
}

// walletSyncState is one stream's ledger_sync row.
type walletSyncState struct {
	oldestID   sql.NullInt64
	newestID   sql.NullInt64
	backfilled bool
}

// observeIDs folds ids into the sync state, returning whether the oldest
// boundary advanced. Callers walking backward use the return value to
// detect a stalled walk (a non-empty page that contained nothing older
// than the cursor already recorded).
func (s *walletSyncState) observeIDs(ids []int64) bool {
	advanced := false
	for _, id := range ids {
		if !s.newestID.Valid || id > s.newestID.Int64 {
			s.newestID = sql.NullInt64{Int64: id, Valid: true}
		}
		if !s.oldestID.Valid || id < s.oldestID.Int64 {
			s.oldestID = sql.NullInt64{Int64: id, Valid: true}
			advanced = true
		}
	}
	return advanced
}

// loadSyncState reads stream's ledger_sync row, or the zero state if the
// stream has never been synced.
func (p *WalletPoller) loadSyncState(ctx context.Context, stream ledgerStream) (walletSyncState, error) {
	var (
		state      walletSyncState
		backfilled int
	)
	err := p.server.db.QueryRowContext(ctx,
		`SELECT oldest_id, newest_id, backfilled FROM ledger_sync WHERE stream = ?`, stream,
	).Scan(&state.oldestID, &state.newestID, &backfilled)
	if errors.Is(err, sql.ErrNoRows) {
		return walletSyncState{}, nil
	}
	if err != nil {
		return walletSyncState{}, fmt.Errorf("loading ledger_sync for %s: %w", stream, err)
	}
	state.backfilled = backfilled != 0
	return state, nil
}

// saveSyncState upserts stream's ledger_sync row.
func (p *WalletPoller) saveSyncState(ctx context.Context, stream ledgerStream, state walletSyncState) error {
	backfilled := 0
	if state.backfilled {
		backfilled = 1
	}
	_, err := p.server.db.ExecContext(ctx, `
		INSERT INTO ledger_sync (stream, oldest_id, newest_id, backfilled, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(stream) DO UPDATE SET
		  oldest_id = excluded.oldest_id, newest_id = excluded.newest_id,
		  backfilled = excluded.backfilled, updated_at = excluded.updated_at`,
		stream, state.oldestID, state.newestID, backfilled, nowUTC(),
	)
	if err != nil {
		return fmt.Errorf("storing ledger_sync for %s: %w", stream, err)
	}
	return nil
}

// transactionIDs extracts the TransactionID of each entry in page.
func transactionIDs(page []esi.WalletTransaction) []int64 {
	ids := make([]int64, len(page))
	for i, tx := range page {
		ids[i] = tx.TransactionID
	}
	return ids
}

// dropTransactionID returns page with the entry matching id removed. It
// undoes ESI's inclusive from_id boundary: a page fetched with from_id=id
// includes the transaction matching id again, which the caller has
// already stored.
func dropTransactionID(page []esi.WalletTransaction, id int64) []esi.WalletTransaction {
	out := make([]esi.WalletTransaction, 0, len(page))
	for _, tx := range page {
		if tx.TransactionID != id {
			out = append(out, tx)
		}
	}
	return out
}
