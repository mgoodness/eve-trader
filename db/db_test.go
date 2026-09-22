package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mgoodness/eve-trader/db"
)

func TestOpenCreatesAllV1Tables(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	want := []string{"market_order", "market_history", "item_type", "character_skill", "esi_token"}
	for _, table := range want {
		var name string
		err := sqlDB.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenCreatesLedgerTables(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	want := []string{"wallet_transaction", "wallet_journal", "ledger_sync", "character_order"}
	for _, table := range want {
		var name string
		err := sqlDB.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenAddsLedgerTablesToAV1_1DatabaseWithoutDataLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	// Build a database in the exact pre-v2 (v1.1) shape by hand -- v1's
	// tables plus the market_history price columns, but none of the v2
	// ledger tables -- so this test proves the migration path against a
	// genuine legacy database, the same way
	// TestOpenMigratesV1MarketHistoryWithoutDataLoss does for
	// market_history, rather than round-tripping through the current
	// (already-updated) db.Open.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE item_type (
			type_id     INTEGER PRIMARY KEY,
			name        TEXT    NOT NULL,
			updated_at  TEXT    NOT NULL
		);
		CREATE TABLE market_order (
			order_id       INTEGER PRIMARY KEY,
			type_id        INTEGER NOT NULL,
			is_buy_order   INTEGER NOT NULL,
			price          REAL    NOT NULL,
			volume_remain  INTEGER NOT NULL,
			volume_total   INTEGER NOT NULL,
			min_volume     INTEGER NOT NULL,
			issued         TEXT    NOT NULL,
			duration       INTEGER NOT NULL,
			updated_at     TEXT    NOT NULL
		);
		CREATE TABLE market_history (
			type_id      INTEGER NOT NULL,
			date         TEXT    NOT NULL,
			volume       INTEGER NOT NULL,
			order_count  INTEGER NOT NULL,
			average      REAL,
			highest      REAL,
			lowest       REAL,
			PRIMARY KEY (type_id, date)
		);
		CREATE TABLE character_skill (
			character_id             INTEGER PRIMARY KEY,
			broker_relations_level   INTEGER NOT NULL,
			accounting_level         INTEGER NOT NULL,
			updated_at               TEXT    NOT NULL
		);
		CREATE TABLE esi_token (
			character_id              INTEGER PRIMARY KEY,
			owner_hash                TEXT    NOT NULL,
			encrypted_refresh_token   BLOB    NOT NULL,
			updated_at                TEXT    NOT NULL
		);
		INSERT INTO item_type (type_id, name, updated_at) VALUES (34, 'Tritanium', '2024-01-01T00:00:00Z');
	`); err != nil {
		legacy.Close()
		t.Fatalf("seeding v1.1 database: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening the legacy database with the current code applies the ledger
	// tables idempotently and leaves the pre-existing data untouched.
	sqlDB, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	var name string
	if err := sqlDB.QueryRow(`SELECT name FROM item_type WHERE type_id = 34`).Scan(&name); err != nil || name != "Tritanium" {
		t.Fatalf("pre-existing item_type row lost: name=%q err=%v", name, err)
	}
	for _, table := range []string{"wallet_transaction", "wallet_journal", "ledger_sync", "character_order"} {
		var count int
		if err := sqlDB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&count); err != nil {
			t.Errorf("table %q not usable after migration: %v", table, err)
		}
	}

	// Reopening applies the same (idempotent) schema again without error.
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer reopened.Close()
}

func TestOpenAddsMarketHistoryPriceColumns(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	// Referencing the columns makes a missing one a query error.
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(average) + COUNT(highest) + COUNT(lowest) FROM market_history`).Scan(&count); err != nil {
		t.Fatalf("market_history price columns missing: %v", err)
	}
}

func TestOpenMigratesV1MarketHistoryWithoutDataLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	// Build a database in its original v1 market_history shape, with a row
	// that predates the price columns.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening legacy database: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE market_history (
			type_id      INTEGER NOT NULL,
			date         TEXT    NOT NULL,
			volume       INTEGER NOT NULL,
			order_count  INTEGER NOT NULL,
			PRIMARY KEY (type_id, date)
		);
		INSERT INTO market_history (type_id, date, volume, order_count) VALUES (34, '2024-01-01', 100, 5);
	`); err != nil {
		legacy.Close()
		t.Fatalf("seeding legacy database: %v", err)
	}
	legacy.Close()

	sqlDB, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	// The pre-existing row survives, with the new price columns NULL until
	// its next history refresh. Selecting the columns also proves the
	// migration added them.
	var (
		volume  int
		average sql.NullFloat64
		highest sql.NullFloat64
		lowest  sql.NullFloat64
	)
	if err := sqlDB.QueryRow(
		`SELECT volume, average, highest, lowest FROM market_history WHERE type_id = 34 AND date = '2024-01-01'`,
	).Scan(&volume, &average, &highest, &lowest); err != nil {
		t.Fatalf("reading migrated row: %v", err)
	}
	if volume != 100 {
		t.Fatalf("volume = %d, want 100 (no data loss)", volume)
	}
	for name, value := range map[string]sql.NullFloat64{"average": average, "highest": highest, "lowest": lowest} {
		if value.Valid {
			t.Errorf("%s = %v, want NULL on a pre-migration row", name, value.Float64)
		}
	}

	// Reopening applies the same migration again without error.
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer reopened.Close()
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	first, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	first.Close()

	second, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close()
}

func TestOpenInMemory(t *testing.T) {
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
