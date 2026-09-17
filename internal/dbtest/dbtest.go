// Package dbtest holds the shared seeding helpers used by every package's
// black-box tests against a real SQLite database (the #11 test-harness
// convention). Kept under internal/ since it's test-support code, not
// part of eve-trader's public surface.
package dbtest

import (
	"database/sql"
	"testing"

	"github.com/mgoodness/eve-trader/db"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
)

// OpenDB opens a fresh in-memory SQLite database with the v1 schema
// applied, closing it automatically at test cleanup.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// SeedItem inserts an item_type row.
func SeedItem(t *testing.T, sqlDB *sql.DB, typeID int, name string) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO item_type (type_id, name, updated_at) VALUES (?, ?, '2024-01-01T00:00:00Z')`,
		typeID, name,
	); err != nil {
		t.Fatalf("seeding item_type %d: %v", typeID, err)
	}
}

// SeedOrder inserts a market_order row for typeID.
func SeedOrder(t *testing.T, sqlDB *sql.DB, orderID int64, typeID int, isBuy bool, price float64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO market_order (order_id, type_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
		 VALUES (?, ?, ?, ?, 1000, 1000, 1, '2024-01-01T00:00:00Z', 90, '2024-01-01T00:00:00Z')`,
		orderID, typeID, isBuy, price,
	); err != nil {
		t.Fatalf("seeding market_order %d: %v", orderID, err)
	}
}

// SeedHistory inserts one market_history row per volume, on consecutive
// early-January 2024 dates (sufficient range for the tests that use it;
// callers needing more than 9 days should extend this helper).
func SeedHistory(t *testing.T, sqlDB *sql.DB, typeID int, volumes ...int) {
	t.Helper()
	for i, v := range volumes {
		date := "2024-01-0" + string(rune('1'+i))
		if _, err := sqlDB.Exec(
			`INSERT INTO market_history (type_id, date, volume, order_count) VALUES (?, ?, ?, 5)`,
			typeID, date, v,
		); err != nil {
			t.Fatalf("seeding market_history for type %d: %v", typeID, err)
		}
	}
}

// SeedSkills inserts the single character_skill row.
func SeedSkills(t *testing.T, sqlDB *sql.DB, characterID, brokerRelations, accounting int) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO character_skill (character_id, broker_relations_level, accounting_level, updated_at)
		 VALUES (?, ?, ?, '2024-01-01T00:00:00Z')`,
		characterID, brokerRelations, accounting,
	); err != nil {
		t.Fatalf("seeding character_skill: %v", err)
	}
}

// SeedToken inserts the single esi_token row, AES-GCM-encrypting
// refreshToken with tokenKey exactly as the auth callback does. Tests that
// exercise authenticated routes (the opportunity table) need this: without a
// stored token the server serves the re-authentication banner instead.
func SeedToken(t *testing.T, sqlDB *sql.DB, characterID int, tokenKey, refreshToken string) {
	t.Helper()
	ciphertext, err := tokencrypt.Encrypt(tokenKey, refreshToken)
	if err != nil {
		t.Fatalf("encrypting refresh token: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at)
		 VALUES (?, 'owner', ?, '2024-01-01T00:00:00Z')`,
		characterID, ciphertext,
	); err != nil {
		t.Fatalf("seeding esi_token: %v", err)
	}
}
