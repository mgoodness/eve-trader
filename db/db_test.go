package db_test

import (
	"database/sql"
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

func TestOpenAddsMarketHistoryPriceColumns(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sqlDB.Close()

	for _, column := range []string{"average", "highest", "lowest"} {
		if !hasColumn(t, sqlDB, "market_history", column) {
			t.Errorf("market_history column %q missing", column)
		}
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

	for _, column := range []string{"average", "highest", "lowest"} {
		if !hasColumn(t, sqlDB, "market_history", column) {
			t.Errorf("market_history column %q missing after migration", column)
		}
	}

	// The pre-existing row survives, with the new price columns NULL until
	// its next history refresh.
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

func hasColumn(t *testing.T, sqlDB *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := sqlDB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
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
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	return false
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
