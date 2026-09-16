// Package poller keeps the current Rens order book in SQLite.
package poller

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

const DefaultInterval = 5 * time.Minute

// Poller periodically replaces the market_order cache with the latest Rens
// snapshot. A failed fetch leaves the previous snapshot untouched.
type Poller struct {
	gateway  esi.ESIGateway
	db       *sql.DB
	interval time.Duration
	mu       sync.Mutex
}

func New(gateway esi.ESIGateway, db *sql.DB, interval time.Duration) *Poller {
	if interval < DefaultInterval {
		interval = DefaultInterval
	}
	return &Poller{gateway: gateway, db: db, interval: interval}
}

// Poll fetches and atomically applies one order-book snapshot.
func (p *Poller) Poll(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	orders, err := p.gateway.FetchRensOrders(ctx)
	if err != nil {
		return fmt.Errorf("fetching Rens orders: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting order poll transaction: %w", err)
	}
	defer tx.Rollback()

	for _, order := range orders {
		name := order.Name
		if name == "" {
			// The gateway may not have names yet. Keep the cache usable until a
			// type-name lookup is added, while still satisfying item_type's
			// non-null display-name contract.
			name = "Type " + strconv.Itoa(order.TypeID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_type (type_id, name, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(type_id) DO UPDATE SET name = CASE WHEN item_type.name LIKE 'Type %' THEN excluded.name ELSE item_type.name END, updated_at = excluded.updated_at`,
			order.TypeID, name, now); err != nil {
			return fmt.Errorf("upserting item type %d: %w", order.TypeID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO market_order (order_id, type_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(order_id) DO UPDATE SET type_id=excluded.type_id, is_buy_order=excluded.is_buy_order, price=excluded.price, volume_remain=excluded.volume_remain, volume_total=excluded.volume_total, min_volume=excluded.min_volume, issued=excluded.issued, duration=excluded.duration, updated_at=excluded.updated_at`,
			order.OrderID, order.TypeID, order.IsBuyOrder, order.Price, order.VolumeRemain, order.VolumeTotal, order.MinVolume, order.Issued.UTC().Format(time.RFC3339Nano), order.Duration, now); err != nil {
			return fmt.Errorf("upserting order %d: %w", order.OrderID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM market_order WHERE updated_at <> ?`, now); err != nil {
		return fmt.Errorf("pruning stale orders: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing order poll: %w", err)
	}
	return nil
}

// Run performs an immediate poll and then polls on the configured cadence.
// It returns when ctx is cancelled. A transient fetch failure is logged and
// retried at the next cadence; the last successful snapshot remains intact.
func (p *Poller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling Rens orders", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling Rens orders", "err", err)
			}
		}
	}
}
