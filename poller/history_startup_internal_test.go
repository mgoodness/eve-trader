package poller

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/db"
)

// openFileDB opens a file-backed test database. A ":memory:" SQLite database
// is private to a single pooled connection, so it is not safe to rely on the
// schema or rows across pooled connections.
func openFileDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// TestWaitForOrderBookReturnsOnceOrdersExist covers the second half of the
// first-boot contract: once the order poller has stored a snapshot, the
// history poller's startup wait lets its refresh proceed.
func TestWaitForOrderBookReturnsOnceOrdersExist(t *testing.T) {
	sqlDB := openFileDB(t)
	if _, err := sqlDB.Exec(`INSERT INTO item_type (type_id, name, updated_at) VALUES (34, 'Tritanium', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO market_order (order_id, type_id, location_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
		VALUES (1, 34, 60004588, 1, 5, 1000, 1000, 1, '2024-01-01T00:00:00Z', 90, '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !waitForOrderBook(ctx, sqlDB, 5*time.Millisecond) {
		t.Fatal("waitForOrderBook() = false, want true once orders exist")
	}
}

// TestWaitForOrderBookStopsOnContextCancel covers the first half: with an
// empty order book the startup wait must not proceed, and a cancelled
// shutdown must unblock it rather than spin until the daily reset.
func TestWaitForOrderBookStopsOnContextCancel(t *testing.T) {
	sqlDB := openFileDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	if waitForOrderBook(ctx, sqlDB, 5*time.Millisecond) {
		t.Fatal("waitForOrderBook() = true on an empty book, want false after cancellation")
	}
}
