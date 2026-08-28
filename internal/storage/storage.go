// Package storage provides SQLite-backed persistence via database/sql and
// modernc.org/sqlite (see ADR-0002 for why this driver over mattn/go-sqlite3).
package storage

import (
	"context"
	"database/sql"
	"fmt"

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

// Ping verifies the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}
