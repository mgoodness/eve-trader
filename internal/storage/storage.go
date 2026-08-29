// Package storage provides SQLite-backed persistence via database/sql and
// modernc.org/sqlite (see ADR-0002 for why this driver over mattn/go-sqlite3).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schema creates every table the app needs if it doesn't already exist, so
// Bootstrap is safe to call on every startup against a persistent file.
//
// oauth_token is a single-row table (see ADR-0003: the refresh token
// rotates on every use, so it lives here rather than in the static secrets
// env file).
const schema = `
CREATE TABLE IF NOT EXISTS oauth_token (
	id            INTEGER PRIMARY KEY CHECK (id = 1),
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	expires_at    TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS order_alert_state (
	order_id      INTEGER NOT NULL,
	alert_type    TEXT NOT NULL,
	last_fired_at TIMESTAMP NOT NULL,
	PRIMARY KEY (order_id, alert_type)
);

CREATE TABLE IF NOT EXISTS alert_feed (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id    INTEGER NOT NULL,
	alert_type  TEXT NOT NULL,
	type_id     INTEGER NOT NULL,
	location_id INTEGER NOT NULL,
	detail      TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS character_skill_cache (
	skill_id   INTEGER PRIMARY KEY,
	level      INTEGER NOT NULL,
	fetched_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS character_standing_cache (
	from_id    INTEGER PRIMARY KEY,
	standing   REAL NOT NULL,
	fetched_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS opportunity_cache (
	hub        TEXT    NOT NULL,
	type_id    INTEGER NOT NULL,
	best_buy   REAL,
	best_sell  REAL,
	avg_volume REAL,
	PRIMARY KEY (hub, type_id)
);

CREATE TABLE IF NOT EXISTS scan_cache_meta (
	hub               TEXT PRIMARY KEY,
	orders_fetched_at TIMESTAMP,
	volume_fetched_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS order_snapshot_cache (
	order_id             INTEGER PRIMARY KEY,
	best_competing_price REAL,
	fetched_at           TIMESTAMP NOT NULL
);
`

// ErrNoToken is returned by LoadToken when no OAuth token has been saved
// yet (i.e. the app hasn't completed a login).
var ErrNoToken = errors.New("storage: no oauth token saved")

// Token is the OAuth token persisted in the single-row oauth_token table.
// The refresh token rotates on every use (see ADR-0003), so SaveToken
// always overwrites the stored value with the newest one.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Store wraps a SQLite-backed *sql.DB.
type Store struct {
	DB *sql.DB
}

// busyTimeoutMS bounds how long a writer that still contends retries
// before failing with SQLITE_BUSY. 5s is a generous margin for this
// single-process, low-request-volume tool's actual write durations
// (single-row upserts/inserts); tune down if writes ever need to fail
// fast instead of briefly blocking a request.
const busyTimeoutMS = 5000

// connPragmas configures every connection modernc.org/sqlite opens for
// this DSN (see its DSN query-parameter handling) with WAL journal mode
// (lets readers and a writer proceed concurrently, instead of the default
// rollback journal's whole-file write lock) and busyTimeoutMS. Without
// these, concurrent writers reliably produce SQLITE_BUSY errors (see
// issue #23).
var connPragmas = fmt.Sprintf("_journal_mode=WAL&_busy_timeout=%d", busyTimeoutMS)

// Open opens (creating if necessary) the SQLite database at path. Callers
// must call Bootstrap before relying on the schema, and Close when done.
func Open(path string) (*Store, error) {
	// path (e.g. from EVE_TRADER_DB_PATH) is concatenated with "?" plus
	// connPragmas to form the driver's DSN; a literal "?" in path would
	// be parsed as the start of that query string instead, silently
	// truncating the path modernc.org/sqlite actually opens.
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("open sqlite database: path %q must not contain '?'", path)
	}

	db, err := sql.Open("sqlite", path+"?"+connPragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	return &Store{DB: db}, nil
}

// Bootstrap creates the app's tables if they don't already exist.
func (s *Store) Bootstrap(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("bootstrap schema: %w", err)
	}
	return nil
}

// SaveToken persists t as the app's single OAuth token, overwriting
// whatever was previously stored.
func (s *Store) SaveToken(ctx context.Context, t Token) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_token (id, access_token, refresh_token, expires_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			access_token  = excluded.access_token,
			refresh_token = excluded.refresh_token,
			expires_at    = excluded.expires_at
	`, t.AccessToken, t.RefreshToken, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save oauth token: %w", err)
	}
	return nil
}

// LoadToken returns the app's stored OAuth token, or ErrNoToken if none has
// been saved yet.
func (s *Store) LoadToken(ctx context.Context) (Token, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT access_token, refresh_token, expires_at FROM oauth_token WHERE id = 1`)

	var t Token
	if err := row.Scan(&t.AccessToken, &t.RefreshToken, &t.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Token{}, ErrNoToken
		}
		return Token{}, fmt.Errorf("load oauth token: %w", err)
	}
	return t, nil
}

// AlertFeedEntry is one historical record in the Alert Feed: a moment an
// Alert actually fired (as opposed to being suppressed by throttling).
type AlertFeedEntry struct {
	ID         int64
	OrderID    int64
	AlertType  string
	TypeID     int32
	LocationID int64
	Detail     string
	CreatedAt  time.Time
}

// GetOrderAlertState returns the last time the given Order+Alert-type
// fired, and whether that Alert is currently considered active (a row
// exists) at all. See ClearOrderAlertState for how a resolved condition
// stops being active.
func (s *Store) GetOrderAlertState(ctx context.Context, orderID int64, alertType string) (lastFiredAt time.Time, exists bool, err error) {
	row := s.DB.QueryRowContext(ctx, `SELECT last_fired_at FROM order_alert_state WHERE order_id = ? AND alert_type = ?`, orderID, alertType)

	if err := row.Scan(&lastFiredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("get order alert state: %w", err)
	}
	return lastFiredAt, true, nil
}

// UpsertOrderAlertState marks an Order+Alert-type as currently active,
// recording firedAt as its most recent firing (for the 4-hour throttle
// window).
func (s *Store) UpsertOrderAlertState(ctx context.Context, orderID int64, alertType string, firedAt time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO order_alert_state (order_id, alert_type, last_fired_at)
		VALUES (?, ?, ?)
		ON CONFLICT (order_id, alert_type) DO UPDATE SET last_fired_at = excluded.last_fired_at
	`, orderID, alertType, firedAt)
	if err != nil {
		return fmt.Errorf("upsert order alert state: %w", err)
	}
	return nil
}

// ClearOrderAlertState marks an Order+Alert-type as no longer active
// (the underlying condition resolved). The next time it becomes true
// again, it's treated as a new detection rather than a suppressed repeat.
func (s *Store) ClearOrderAlertState(ctx context.Context, orderID int64, alertType string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM order_alert_state WHERE order_id = ? AND alert_type = ?`, orderID, alertType); err != nil {
		return fmt.Errorf("clear order alert state: %w", err)
	}
	return nil
}

// InsertAlertFeedEntry records a fired Alert in the chronological Alert
// Feed.
func (s *Store) InsertAlertFeedEntry(ctx context.Context, e AlertFeedEntry) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO alert_feed (order_id, alert_type, type_id, location_id, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.OrderID, e.AlertType, e.TypeID, e.LocationID, e.Detail, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert alert feed entry: %w", err)
	}
	return nil
}

// RecentAlertFeed returns up to limit Alert Feed entries, most recent
// first.
func (s *Store) RecentAlertFeed(ctx context.Context, limit int) ([]AlertFeedEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, order_id, alert_type, type_id, location_id, detail, created_at
		FROM alert_feed
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query alert feed: %w", err)
	}
	defer rows.Close()

	var entries []AlertFeedEntry
	for rows.Next() {
		var e AlertFeedEntry
		if err := rows.Scan(&e.ID, &e.OrderID, &e.AlertType, &e.TypeID, &e.LocationID, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert feed entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert feed: %w", err)
	}
	return entries, nil
}

// Ping verifies the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}
