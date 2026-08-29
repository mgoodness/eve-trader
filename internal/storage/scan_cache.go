package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SaveCharacterSkills persists the character's skill levels (keyed by
// ESI skill_id), replacing whatever was previously cached (see
// ADR-0001/ADR-0006: this feeds Fee computation, cached 24h).
func (s *Store) SaveCharacterSkills(ctx context.Context, levels map[int32]int32, fetchedAt time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save character skills: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM character_skill_cache`); err != nil {
		return fmt.Errorf("save character skills: clear: %w", err)
	}
	for skillID, level := range levels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO character_skill_cache (skill_id, level, fetched_at) VALUES (?, ?, ?)`, skillID, level, fetchedAt); err != nil {
			return fmt.Errorf("save character skills: insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save character skills: commit: %w", err)
	}
	return nil
}

// LoadCharacterSkills returns the cached skill levels (keyed by ESI
// skill_id) and when they were fetched. ok is false if nothing has been
// cached yet.
func (s *Store) LoadCharacterSkills(ctx context.Context) (levels map[int32]int32, fetchedAt time.Time, ok bool, err error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT skill_id, level, fetched_at FROM character_skill_cache`)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("load character skills: %w", err)
	}
	defer rows.Close()

	levels = make(map[int32]int32)
	for rows.Next() {
		var skillID, level int32
		var t time.Time
		if err := rows.Scan(&skillID, &level, &t); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("load character skills: scan: %w", err)
		}
		levels[skillID] = level
		fetchedAt = t
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("load character skills: iterate: %w", err)
	}
	return levels, fetchedAt, len(levels) > 0, nil
}

// SaveCharacterStandings persists the character's standings (keyed by
// ESI from_id), replacing whatever was previously cached.
func (s *Store) SaveCharacterStandings(ctx context.Context, standings map[int32]float64, fetchedAt time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save character standings: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM character_standing_cache`); err != nil {
		return fmt.Errorf("save character standings: clear: %w", err)
	}
	for fromID, standing := range standings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO character_standing_cache (from_id, standing, fetched_at) VALUES (?, ?, ?)`, fromID, standing, fetchedAt); err != nil {
			return fmt.Errorf("save character standings: insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save character standings: commit: %w", err)
	}
	return nil
}

// LoadCharacterStandings returns the cached standings (keyed by ESI
// from_id) and when they were fetched. ok is false if nothing has been
// cached yet.
func (s *Store) LoadCharacterStandings(ctx context.Context) (standings map[int32]float64, fetchedAt time.Time, ok bool, err error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT from_id, standing, fetched_at FROM character_standing_cache`)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("load character standings: %w", err)
	}
	defer rows.Close()

	standings = make(map[int32]float64)
	for rows.Next() {
		var fromID int32
		var standing float64
		var t time.Time
		if err := rows.Scan(&fromID, &standing, &t); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("load character standings: scan: %w", err)
		}
		standings[fromID] = standing
		fetchedAt = t
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("load character standings: iterate: %w", err)
	}
	return standings, fetchedAt, len(standings) > 0, nil
}

// ScanCacheMeta tracks a Hub's Scan Cycle freshness: OrdersFetchedAt (the
// 5-minute order-book cache window) and VolumeFetchedAt (the 24-hour
// market-history cache window), each governing when the corresponding
// half of opportunity_cache needs refreshing.
type ScanCacheMeta struct {
	Hub             string
	OrdersFetchedAt time.Time
	VolumeFetchedAt time.Time
}

// LoadScanCacheMeta returns hub's cache freshness. ok is false if this
// Hub has never been scanned.
func (s *Store) LoadScanCacheMeta(ctx context.Context, hub string) (ScanCacheMeta, bool, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT orders_fetched_at, volume_fetched_at FROM scan_cache_meta WHERE hub = ?`, hub)

	meta := ScanCacheMeta{Hub: hub}
	if err := row.Scan(&meta.OrdersFetchedAt, &meta.VolumeFetchedAt); err != nil {
		if err == sql.ErrNoRows {
			return ScanCacheMeta{}, false, nil
		}
		return ScanCacheMeta{}, false, fmt.Errorf("load scan cache meta: %w", err)
	}
	return meta, true, nil
}

// SaveScanCacheMeta upserts meta.Hub's cache freshness timestamps.
func (s *Store) SaveScanCacheMeta(ctx context.Context, meta ScanCacheMeta) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO scan_cache_meta (hub, orders_fetched_at, volume_fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT (hub) DO UPDATE SET
			orders_fetched_at = excluded.orders_fetched_at,
			volume_fetched_at = excluded.volume_fetched_at
	`, meta.Hub, meta.OrdersFetchedAt, meta.VolumeFetchedAt)
	if err != nil {
		return fmt.Errorf("save scan cache meta: %w", err)
	}
	return nil
}

// OpportunityPrice is one item's cached best buy/sell price at a Hub.
// Nil means no order exists on that side, distinct from a price of zero.
type OpportunityPrice struct {
	BestBuy  *float64
	BestSell *float64
}

// OpportunityCacheEntry is one item's cached scan data at a Hub: prices
// (refreshed on the 5-minute order-book cycle) and average daily volume
// (refreshed on the 24-hour market-history cycle).
type OpportunityCacheEntry struct {
	BestBuy   *float64
	BestSell  *float64
	AvgVolume *float64
}

// SaveOpportunityPrices upserts each item's best buy/sell price at hub,
// leaving any previously-cached AvgVolume for that item untouched.
func (s *Store) SaveOpportunityPrices(ctx context.Context, hub string, prices map[int32]OpportunityPrice) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save opportunity prices: begin: %w", err)
	}
	defer tx.Rollback()

	for typeID, p := range prices {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO opportunity_cache (hub, type_id, best_buy, best_sell)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (hub, type_id) DO UPDATE SET
				best_buy  = excluded.best_buy,
				best_sell = excluded.best_sell
		`, hub, typeID, p.BestBuy, p.BestSell)
		if err != nil {
			return fmt.Errorf("save opportunity prices: upsert type %d: %w", typeID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save opportunity prices: commit: %w", err)
	}
	return nil
}

// SaveOpportunityVolumes upserts each item's average daily volume at
// hub, leaving any previously-cached prices for that item untouched.
func (s *Store) SaveOpportunityVolumes(ctx context.Context, hub string, volumes map[int32]float64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save opportunity volumes: begin: %w", err)
	}
	defer tx.Rollback()

	for typeID, vol := range volumes {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO opportunity_cache (hub, type_id, avg_volume)
			VALUES (?, ?, ?)
			ON CONFLICT (hub, type_id) DO UPDATE SET avg_volume = excluded.avg_volume
		`, hub, typeID, vol)
		if err != nil {
			return fmt.Errorf("save opportunity volumes: upsert type %d: %w", typeID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save opportunity volumes: commit: %w", err)
	}
	return nil
}

// LoadOpportunityCache returns hub's cached scan data, keyed by type_id.
func (s *Store) LoadOpportunityCache(ctx context.Context, hub string) (map[int32]OpportunityCacheEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT type_id, best_buy, best_sell, avg_volume FROM opportunity_cache WHERE hub = ?`, hub)
	if err != nil {
		return nil, fmt.Errorf("load opportunity cache: %w", err)
	}
	defer rows.Close()

	entries := make(map[int32]OpportunityCacheEntry)
	for rows.Next() {
		var typeID int32
		var e OpportunityCacheEntry
		if err := rows.Scan(&typeID, &e.BestBuy, &e.BestSell, &e.AvgVolume); err != nil {
			return nil, fmt.Errorf("load opportunity cache: scan: %w", err)
		}
		entries[typeID] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load opportunity cache: iterate: %w", err)
	}
	return entries, nil
}

// SaveOrderSnapshot upserts orderID's cached competing-price snapshot
// (see tracker.MarketSnapshot): bestCompetingPrice nil means no
// competing order was found, distinct from a price of zero.
func (s *Store) SaveOrderSnapshot(ctx context.Context, orderID int64, bestCompetingPrice *float64, fetchedAt time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO order_snapshot_cache (order_id, best_competing_price, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT (order_id) DO UPDATE SET
			best_competing_price = excluded.best_competing_price,
			fetched_at           = excluded.fetched_at
	`, orderID, bestCompetingPrice, fetchedAt)
	if err != nil {
		return fmt.Errorf("save order snapshot: %w", err)
	}
	return nil
}

// LoadOrderSnapshot returns orderID's cached competing-price snapshot and
// when it was fetched. ok is false if this Order has never been
// snapshotted.
func (s *Store) LoadOrderSnapshot(ctx context.Context, orderID int64) (bestCompetingPrice *float64, fetchedAt time.Time, ok bool, err error) {
	row := s.DB.QueryRowContext(ctx, `SELECT best_competing_price, fetched_at FROM order_snapshot_cache WHERE order_id = ?`, orderID)

	if err := row.Scan(&bestCompetingPrice, &fetchedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, false, fmt.Errorf("load order snapshot: %w", err)
	}
	return bestCompetingPrice, fetchedAt, true, nil
}
