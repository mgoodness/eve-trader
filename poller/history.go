package poller

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

const (
	// HistoryRetention is the number of calendar days retained in market_history.
	HistoryRetention = 14
	// HistoryResetHour and HistoryResetMinute are ESI's approximate daily cache
	// reset time. Refreshes are scheduled just after this point.
	HistoryResetHour   = 11
	HistoryResetMinute = 5
)

// HistoryPoller refreshes the rolling market history window for every type
// currently present in market_order. A failed refresh leaves the prior window
// intact.
type HistoryPoller struct {
	gateway esi.ESIGateway
	db      *sql.DB
	mu      sync.Mutex
}

func NewHistory(gateway esi.ESIGateway, db *sql.DB) *HistoryPoller {
	return &HistoryPoller{gateway: gateway, db: db}
}

// Poll fetches history for each distinct type on the current order book and
// atomically applies it. The current UTC date is retained plus the preceding
// 13 calendar dates.
func (p *HistoryPoller) Poll(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	rows, err := p.db.QueryContext(ctx, `SELECT DISTINCT type_id FROM market_order ORDER BY type_id`)
	if err != nil {
		return fmt.Errorf("finding market-history type IDs: %w", err)
	}
	var typeIDs []int
	for rows.Next() {
		var typeID int
		if err := rows.Scan(&typeID); err != nil {
			rows.Close()
			return fmt.Errorf("scanning market-history type ID: %w", err)
		}
		typeIDs = append(typeIDs, typeID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading market-history type IDs: %w", err)
	}
	rows.Close()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	cutoff := today.AddDate(0, 0, -(HistoryRetention - 1))
	history := make(map[int][]esi.HistoryPoint, len(typeIDs))
	for _, typeID := range typeIDs {
		points, err := p.gateway.FetchHistory(ctx, typeID)
		if err != nil {
			return fmt.Errorf("fetching history for type %d: %w", typeID, err)
		}
		history[typeID] = points
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting history poll transaction: %w", err)
	}
	defer tx.Rollback()
	for typeID, points := range history {
		// Replace the retained slice for this type so a day omitted by ESI is
		// not accidentally kept forever from an earlier refresh.
		if _, err := tx.ExecContext(ctx, `DELETE FROM market_history WHERE type_id = ? AND date >= ?`, typeID, cutoff.Format("2006-01-02")); err != nil {
			return fmt.Errorf("resetting history for type %d: %w", typeID, err)
		}
		for _, point := range points {
			date := point.Date.UTC().Format("2006-01-02")
			if point.Date.UTC().Before(cutoff) || date > today.Format("2006-01-02") {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_history (type_id, date, volume, order_count) VALUES (?, ?, ?, ?)
				ON CONFLICT(type_id, date) DO UPDATE SET volume=excluded.volume, order_count=excluded.order_count`,
				typeID, date, point.Volume, point.OrderCount); err != nil {
				return fmt.Errorf("upserting history for type %d on %s: %w", typeID, date, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM market_history WHERE date < ?`, cutoff.Format("2006-01-02")); err != nil {
		return fmt.Errorf("pruning market history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing history poll: %w", err)
	}
	return nil
}

// Run performs an immediate refresh and then refreshes daily. It is intended
// to be started as a goroutine alongside the order-book poller.
func (p *HistoryPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling market history", "err", err)
	}
	timer := time.NewTimer(time.Until(nextHistoryRefresh(time.Now().UTC())))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling market history", "err", err)
			}
			timer.Reset(time.Until(nextHistoryRefresh(time.Now().UTC())))
		}
	}
}

func nextHistoryRefresh(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), HistoryResetHour, HistoryResetMinute+1, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
