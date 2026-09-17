package poller

import (
	"context"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
)

// TestWaitForOrderBookReturnsWhenBookArrives covers the first-boot race: the
// history poller starts before the order poller has stored its initial
// snapshot, so its first refresh must wait for the book rather than no-op
// until the next daily reset.
func TestWaitForOrderBookReturnsWhenBookArrives(t *testing.T) {
	database := dbtest.OpenDB(t)
	ctx := t.Context()

	go func() {
		time.Sleep(20 * time.Millisecond)
		// Raw Exec so a failure doesn't call t.Fatalf off the test goroutine.
		database.Exec(`INSERT INTO item_type (type_id, name, updated_at) VALUES (34, 'Tritanium', '2024-01-01T00:00:00Z')`)
		database.Exec(`INSERT INTO market_order (order_id, type_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
			VALUES (1, 34, 1, 5, 1000, 1000, 1, '2024-01-01T00:00:00Z', 90, '2024-01-01T00:00:00Z')`)
	}()

	if !waitForOrderBook(ctx, database, 5*time.Millisecond) {
		t.Fatal("waitForOrderBook() = false, want true once orders exist")
	}
}

// TestWaitForOrderBookStopsOnContextCancel keeps a cancelled shutdown from
// spinning forever while the book is still empty.
func TestWaitForOrderBookStopsOnContextCancel(t *testing.T) {
	database := dbtest.OpenDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	if waitForOrderBook(ctx, database, 5*time.Millisecond) {
		t.Fatal("waitForOrderBook() = true on an empty book, want false after cancellation")
	}
}
