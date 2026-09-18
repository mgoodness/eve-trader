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
// v1 has exactly one schema, applied in full on every Open; additive
// changes made after v1 are applied by migrate on top. When the schema
// needs a change beyond adding a column, introduce a numbered-migrations
// table at that point rather than building a migration framework ahead of
// the need.
func Open(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dsn, err)
	}

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("applying schema to %s: %w", dsn, err)
	}
	if err := migrate(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrating database %s: %w", dsn, err)
	}

	return sqlDB, nil
}

// migrate applies the additive schema changes made since the initial v1
// schema, so an existing database upgrades in place with no data loss.
// SQLite has no ADD COLUMN IF NOT EXISTS, so the table's current columns
// are read and only the missing ones are added; the call is idempotent.
func migrate(sqlDB *sql.DB) error {
	existing, err := tableColumns(sqlDB, "market_history")
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, typ string }{
		{"average", "REAL"},
		{"highest", "REAL"},
		{"lowest", "REAL"},
	} {
		if existing[col.name] {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf("ALTER TABLE market_history ADD COLUMN %s %s", col.name, col.typ)); err != nil {
			return fmt.Errorf("adding market_history.%s: %w", col.name, err)
		}
	}
	return nil
}

// tableColumns returns the set of column names on table.
func tableColumns(sqlDB *sql.DB, table string) (map[string]bool, error) {
	// PRAGMA does not accept a bound parameter for the table name; table is
	// always an internal constant, never user input.
	rows, err := sqlDB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("reading columns of %s: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid          int
			name         string
			typ          string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("scanning columns of %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading columns of %s: %w", table, err)
	}
	return columns, nil
}
