package db_test

import (
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
