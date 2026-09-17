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

	names, err := p.resolveTypeNames(ctx, orders)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting order poll transaction: %w", err)
	}
	defer tx.Rollback()

	for _, order := range orders {
		name := names[order.TypeID]
		if name == "" {
			// The gateway could not name this type. Keep the cache usable while
			// still satisfying item_type's non-null display-name contract; the
			// "Type " prefix marks the row for retry on a later poll.
			name = "Type " + strconv.Itoa(order.TypeID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_type (type_id, name, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(type_id) DO UPDATE SET name = excluded.name, updated_at = excluded.updated_at`,
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

// resolveTypeNames returns a display name for every distinct type_id in
// orders. Names already cached in item_type are reused; only the remaining
// type_ids are resolved, through one batched gateway call. A type the
// gateway cannot name is left out of the map so the caller can fall back.
func (p *Poller) resolveTypeNames(ctx context.Context, orders []esi.Order) (map[int]string, error) {
	names, err := p.cachedTypeNames(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[int]bool, len(orders))
	var missing []int
	for _, order := range orders {
		if seen[order.TypeID] {
			continue
		}
		seen[order.TypeID] = true
		if _, ok := names[order.TypeID]; !ok {
			missing = append(missing, order.TypeID)
		}
	}
	if len(missing) == 0 {
		return names, nil
	}

	resolved, err := p.gateway.FetchTypeNames(ctx, missing)
	if err != nil {
		return nil, fmt.Errorf("resolving type names: %w", err)
	}
	for id, name := range resolved {
		names[id] = name
	}
	return names, nil
}

// cachedTypeNames loads the item_type rows the poller can trust -- those
// with a real display name. Rows still holding the "Type <id>" fallback
// are omitted so a later poll retries them.
func (p *Poller) cachedTypeNames(ctx context.Context) (map[int]string, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT type_id, name FROM item_type WHERE name NOT LIKE 'Type %'`)
	if err != nil {
		return nil, fmt.Errorf("loading cached type names: %w", err)
	}
	defer rows.Close()

	names := make(map[int]string)
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scanning cached type name: %w", err)
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading cached type names: %w", err)
	}
	return names, nil
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
