// Package db opens the eve-trader SQLite database and ensures the schema
// (v1's tables, see docs/spec/v1.md §5, plus the v2 ledger tables added
// in schema.sql, see docs/spec/v2.md §5) is present.
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
	// The v1.1 market_history window predates its price columns; the v2
	// ledger predates Advanced Broker Relations in character_skill; the
	// region-wide order book predates location_id in market_order. Each
	// addition is applied only when missing, so migrate is idempotent.
	const rensStationID = 60004588
	additions := []struct {
		table, name, typ string
	}{
		{"market_history", "average", "REAL"},
		{"market_history", "highest", "REAL"},
		{"market_history", "lowest", "REAL"},
		{"character_skill", "advanced_broker_relations_level", "INTEGER NOT NULL DEFAULT 0"},
		// Pre-region market_order held only Rens rows, so backfilling the
		// new column with Rens is the correct default for an existing cache.
		{"market_order", "location_id", fmt.Sprintf("INTEGER NOT NULL DEFAULT %d", rensStationID)},
	}
	columns := map[string]map[string]bool{}
	for _, add := range additions {
		if columns[add.table] == nil {
			existing, err := tableColumns(sqlDB, add.table)
			if err != nil {
				return err
			}
			columns[add.table] = existing
		}
		if columns[add.table][add.name] {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", add.table, add.name, add.typ)); err != nil {
			return fmt.Errorf("adding %s.%s: %w", add.table, add.name, err)
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
