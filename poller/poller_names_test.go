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

// namesGateway records which type IDs the poller asked it to resolve, so
// tests can assert the item_type cache spares known types.
type namesGateway struct {
	orders []esi.Order
	names  map[int]string

	requested [][]int
}

func (g *namesGateway) FetchRensOrders(context.Context) ([]esi.Order, error) {
	return g.orders, nil
}

func (g *namesGateway) FetchTypeNames(_ context.Context, typeIDs []int) (map[int]string, error) {
	g.requested = append(g.requested, append([]int(nil), typeIDs...))
	out := make(map[int]string, len(typeIDs))
	for _, id := range typeIDs {
		if name, ok := g.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

func (*namesGateway) FetchHistory(context.Context, int) ([]esi.HistoryPoint, error) {
	return nil, nil
}
func (*namesGateway) FetchCharacterSkills(context.Context, int, string) (esi.Skills, error) {
	return esi.Skills{}, nil
}
func (*namesGateway) FetchWalletTransactions(context.Context, int, string, int64) ([]esi.WalletTransaction, error) {
	return nil, nil
}
func (*namesGateway) FetchWalletJournal(context.Context, int, string) ([]esi.WalletJournalEntry, error) {
	return nil, nil
}
func (*namesGateway) FetchCharacterOrders(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*namesGateway) FetchCharacterOrderHistory(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*namesGateway) ExchangeCode(context.Context, string, string) (esi.Token, error) {
	return esi.Token{}, nil
}
func (*namesGateway) RefreshToken(context.Context, string) (esi.Token, error) {
	return esi.Token{}, nil
}

func TestPollResolvesOnlyTypesMissingFromItemType(t *testing.T) {
	database := dbtest.OpenDB(t)
	dbtest.SeedItem(t, database, 34, "Tritanium")

	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gateway := &namesGateway{
		orders: []esi.Order{
			{OrderID: 1, TypeID: 34, Issued: issued},
			{OrderID: 2, TypeID: 35, Issued: issued},
			{OrderID: 3, TypeID: 35, Issued: issued},
		},
		names: map[int]string{35: "Pyerite"},
	}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	if want := [][]int{{35}}; !reflect.DeepEqual(gateway.requested, want) {
		t.Fatalf("FetchTypeNames calls = %v, want %v", gateway.requested, want)
	}
	assertItemName(t, database, 34, "Tritanium")
	assertItemName(t, database, 35, "Pyerite")
}

func TestPollSkipsNameResolutionWhenAllTypesAreCached(t *testing.T) {
	database := dbtest.OpenDB(t)
	dbtest.SeedItem(t, database, 34, "Tritanium")

	gateway := &namesGateway{orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: time.Now()}}}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	if len(gateway.requested) != 0 {
		t.Fatalf("FetchTypeNames calls = %v, want none", gateway.requested)
	}
	assertItemName(t, database, 34, "Tritanium")
}

func TestPollRetriesFallbackNames(t *testing.T) {
	database := dbtest.OpenDB(t)
	dbtest.SeedItem(t, database, 34, "Type 34")

	gateway := &namesGateway{
		orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: time.Now()}},
		names:  map[int]string{34: "Tritanium"},
	}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	if want := [][]int{{34}}; !reflect.DeepEqual(gateway.requested, want) {
		t.Fatalf("FetchTypeNames calls = %v, want %v", gateway.requested, want)
	}
	assertItemName(t, database, 34, "Tritanium")
}

func assertItemName(t *testing.T, database *sql.DB, typeID int, want string) {
	t.Helper()
	var name string
	if err := database.QueryRow(`SELECT name FROM item_type WHERE type_id = ?`, typeID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != want {
		t.Fatalf("item_type %d name = %q, want %q", typeID, name, want)
	}
}
