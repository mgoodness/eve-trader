package poller_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/poller"
)

type scriptedGateway struct {
	snapshots [][]esi.Order
	calls     int
}

func (g *scriptedGateway) FetchRensOrders(_ context.Context) ([]esi.Order, error) {
	orders := g.snapshots[g.calls]
	g.calls++
	return orders, nil
}
func (*scriptedGateway) FetchTypeNames(context.Context, []int) (map[int]string, error) {
	return nil, nil
}
func (*scriptedGateway) FetchHistory(context.Context, int) ([]esi.HistoryPoint, error) {
	return nil, nil
}
func (*scriptedGateway) FetchCharacterSkills(context.Context, int, string) (esi.Skills, error) {
	return esi.Skills{}, nil
}
func (*scriptedGateway) FetchWalletTransactions(context.Context, int, string, int64) ([]esi.WalletTransaction, error) {
	return nil, nil
}
func (*scriptedGateway) FetchWalletJournal(context.Context, int, string) ([]esi.WalletJournalEntry, error) {
	return nil, nil
}
func (*scriptedGateway) FetchCharacterOrders(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*scriptedGateway) FetchCharacterOrderHistory(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*scriptedGateway) ExchangeCode(context.Context, string, string) (esi.Token, error) {
	return esi.Token{}, nil
}
func (*scriptedGateway) RefreshToken(context.Context, string) (esi.Token, error) {
	return esi.Token{}, nil
}

func TestPollReplacesAndPrunesSnapshot(t *testing.T) {
	database := dbtest.OpenDB(t)
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gateway := &scriptedGateway{snapshots: [][]esi.Order{
		{{OrderID: 1, TypeID: 34, IsBuyOrder: true, Price: 5, Issued: issued}, {OrderID: 2, TypeID: 35, Price: 8, Issued: issued}},
		{{OrderID: 1, TypeID: 34, IsBuyOrder: true, Price: 6, Issued: issued}, {OrderID: 3, TypeID: 36, Price: 9, Issued: issued}},
	}}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertOrderIDs(t, database, []int64{1, 2})
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM market_order`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("market_order count = %d, want 2", count)
	}
	assertOrderIDs(t, database, []int64{1, 3})
	var price float64
	if err := database.QueryRow(`SELECT price FROM market_order WHERE order_id = 1`).Scan(&price); err != nil {
		t.Fatal(err)
	}
	if price != 6 {
		t.Fatalf("updated price = %v, want 6", price)
	}
	var name string
	if err := database.QueryRow(`SELECT name FROM item_type WHERE type_id = 36`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Type 36" {
		t.Fatalf("lazy name = %q, want fallback", name)
	}
}

func assertOrderIDs(t *testing.T, database *sql.DB, want []int64) {
	t.Helper()
	rows, err := database.Query(`SELECT order_id FROM market_order ORDER BY order_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order IDs = %v, want %v", got, want)
	}
}
