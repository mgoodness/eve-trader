// orders.go implements the character-orders-ledger poller
// (docs/spec/v2.md §3, §5): it fetches the character's open orders and
// cancelled/expired order history through ESIGateway and appends each
// snapshot to character_order, keyed by (order_id, issued). Because the key
// includes issued, an in-place modify (same order_id, a new issued) and a
// cancel-and-recreate re-list both leave their prior snapshots behind, so
// re-list chains stay inferable on read.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

// CharacterOrdersInterval is how often CharacterOrderPoller refreshes the
// character's orders, matching docs/spec/v2.md §5's ~20 min orders cadence.
const CharacterOrdersInterval = 20 * time.Minute

// CharacterOrderPoller keeps character_order current with the character's
// open orders and order history. It shares the server's token-refresh and
// re-authentication path exactly like WalletPoller.
type CharacterOrderPoller struct {
	server   *Server
	interval time.Duration
}

// NewCharacterOrderPoller creates a background character-orders poller.
func (s *Server) NewCharacterOrderPoller(interval time.Duration) *CharacterOrderPoller {
	if interval <= 0 {
		interval = CharacterOrdersInterval
	}
	return &CharacterOrderPoller{server: s, interval: interval}
}

// Poll refreshes the stored token and snapshots both order streams. No
// stored token means there is nothing to poll yet (the first-boot state):
// that is a no-op, not a polling failure, exactly like WalletPoller.
func (p *CharacterOrderPoller) Poll(ctx context.Context) error {
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

	// Neither order route has a from_id cursor: the open route returns
	// current state and the history route a fixed ~90-day window, so each
	// poll re-reads the whole window and there is nothing for ledger_sync to
	// bound. Snapshots are keyed by (order_id, issued) instead.
	open, err := p.server.gateway.FetchCharacterOrders(ctx, characterID, token.AccessToken)
	if err != nil {
		p.server.latchIfInsufficientScope(err)
		return fmt.Errorf("fetching open orders: %w", err)
	}
	history, err := p.server.gateway.FetchCharacterOrderHistory(ctx, characterID, token.AccessToken)
	if err != nil {
		p.server.latchIfInsufficientScope(err)
		return fmt.Errorf("fetching order history: %w", err)
	}
	if err := p.upsertOrders(ctx, open); err != nil {
		return err
	}
	if err := p.upsertOrders(ctx, history); err != nil {
		return err
	}
	return nil
}

// Run performs an immediate snapshot and then polls on the configured
// cadence.
func (p *CharacterOrderPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling character orders", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling character orders", "err", err)
			}
		}
	}
}

// upsertOrders writes orders to character_order in one transaction. The
// conflict target is the full (order_id, issued) snapshot key: a row for
// the same order_id with a new issued is a distinct, additional snapshot,
// not an overwrite (docs/spec/v2.md §3).
func (p *CharacterOrderPoller) upsertOrders(ctx context.Context, orders []esi.CharacterOrder) error {
	if len(orders) == 0 {
		return nil
	}
	tx, err := p.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting character order upsert: %w", err)
	}
	defer tx.Rollback()
	for _, o := range orders {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO character_order (order_id, issued, type_id, location_id, is_buy_order, price, volume_remain, volume_total, min_volume, duration, state, is_corporation, region_id, order_range, escrow, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(order_id, issued) DO UPDATE SET
			  type_id = excluded.type_id, location_id = excluded.location_id, is_buy_order = excluded.is_buy_order,
			  price = excluded.price, volume_remain = excluded.volume_remain, volume_total = excluded.volume_total,
			  min_volume = excluded.min_volume, duration = excluded.duration, state = excluded.state,
			  is_corporation = excluded.is_corporation, region_id = excluded.region_id, order_range = excluded.order_range,
			  escrow = excluded.escrow, updated_at = excluded.updated_at`,
			o.OrderID, o.Issued.UTC().Format(time.RFC3339Nano), o.TypeID, o.LocationID, o.IsBuyOrder, o.Price,
			o.VolumeRemain, o.VolumeTotal, o.MinVolume, o.Duration, o.State, o.IsCorporation, o.RegionID, o.Range, o.Escrow, nowUTC(),
		); err != nil {
			return fmt.Errorf("upserting character order %d issued %s: %w", o.OrderID, o.Issued.UTC().Format(time.RFC3339Nano), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing character order upsert: %w", err)
	}
	return nil
}
