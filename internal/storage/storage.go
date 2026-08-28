// Package storage provides SQLite-backed persistence via database/sql and
// modernc.org/sqlite (see ADR-0002 for why this driver over mattn/go-sqlite3).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Open opens (creating if necessary) the SQLite database at path. Callers
// must call Bootstrap before relying on the schema, and Close when done.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
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

// Ping verifies the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}
