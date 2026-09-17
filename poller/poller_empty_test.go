package poller_test

import (
	"context"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/poller"
)

func TestPollEmptySnapshotRemovesAllOrders(t *testing.T) {
	database := dbtest.OpenDB(t)
	gateway := &failingOrdersGateway{orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: time.Now()}}}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	gateway.orders = nil
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM market_order`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("market_order count = %d, want empty snapshot", count)
	}
}
