// Package db opens the eve-trader SQLite database and ensures the v1
// schema (see docs/spec/v1.md §5) is present.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Open opens (creating if necessary) the SQLite database at dsn and
// applies the v1 schema. dsn may be a file path or ":memory:". The schema
// is idempotent (CREATE TABLE IF NOT EXISTS), so Open is safe to call
// against an existing database.
//
// v1 has exactly one schema, applied in full on every Open; there is no
// migration runner. When the schema needs to change, either add
// idempotent ALTER statements here or introduce a numbered-migrations
// table at that point, rather than building a migration framework ahead
// of the need.
func Open(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dsn, err)
	}

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("applying schema to %s: %w", dsn, err)
	}

	return sqlDB, nil
}
