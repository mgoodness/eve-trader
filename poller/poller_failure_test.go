package poller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/poller"
)

type failingOrdersGateway struct {
	orders []esi.Order
	err    error
}

func (g *failingOrdersGateway) FetchRegionOrders(context.Context) ([]esi.Order, error) {
	return g.orders, g.err
}
func (*failingOrdersGateway) FetchTypeNames(context.Context, []int) (map[int]string, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchHistory(context.Context, int) ([]esi.HistoryPoint, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchCharacterSkills(context.Context, int, string) (esi.Skills, error) {
	return esi.Skills{}, nil
}
func (*failingOrdersGateway) FetchCharacterStandings(context.Context, int, string) ([]esi.Standing, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchStationOwner(context.Context, int64) (int64, error) { return 0, nil }
func (*failingOrdersGateway) FetchWalletTransactions(context.Context, int, string, int64) ([]esi.WalletTransaction, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchWalletJournal(context.Context, int, string) ([]esi.WalletJournalEntry, error) {
	return nil, nil
}

func (*failingOrdersGateway) FetchCharacterContracts(context.Context, int, string) ([]esi.Contract, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchContractItems(context.Context, int, string, int64) ([]esi.ContractItem, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchCharacterOrders(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*failingOrdersGateway) FetchCharacterOrderHistory(context.Context, int, string) ([]esi.CharacterOrder, error) {
	return nil, nil
}
func (*failingOrdersGateway) ExchangeCode(context.Context, string, string) (esi.Token, error) {
	return esi.Token{}, nil
}
func (*failingOrdersGateway) RefreshToken(context.Context, string) (esi.Token, error) {
	return esi.Token{}, nil
}

func TestPollFailureLeavesPreviousSnapshot(t *testing.T) {
	database := dbtest.OpenDB(t)
	gateway := &failingOrdersGateway{orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: time.Now()}}}
	p := poller.New(gateway, database, time.Hour)
	if err := p.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	gateway.orders = nil
	gateway.err = errors.New("ESI unavailable")
	if err := p.Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want fetch failure")
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM market_order`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("market_order count = %d, want previous snapshot preserved", count)
	}
}
