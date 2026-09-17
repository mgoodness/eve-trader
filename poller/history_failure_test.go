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

type failingHistoryGateway struct {
	history map[int][]esi.HistoryPoint
	err     error
}

func (*failingHistoryGateway) FetchRensOrders(context.Context) ([]esi.Order, error) {
	return nil, nil
}
func (*failingHistoryGateway) FetchTypeNames(context.Context, []int) (map[int]string, error) {
	return nil, nil
}
func (g *failingHistoryGateway) FetchHistory(context.Context, int) ([]esi.HistoryPoint, error) {
	return g.history[34], g.err
}
func (*failingHistoryGateway) FetchCharacterSkills(context.Context, int, string) (esi.Skills, error) {
	return esi.Skills{}, nil
}
func (*failingHistoryGateway) ExchangeCode(context.Context, string, string) (esi.Token, error) {
	return esi.Token{}, nil
}
func (*failingHistoryGateway) RefreshToken(context.Context, string) (esi.Token, error) {
	return esi.Token{}, nil
}

func TestHistoryPollFailureLeavesPreviousWindow(t *testing.T) {
	database := dbtest.OpenDB(t)
	orders := poller.New(&failingOrdersGateway{orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: time.Now()}}}, database, time.Hour)
	if err := orders.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	gateway := &failingHistoryGateway{history: map[int][]esi.HistoryPoint{34: {{Date: time.Now().UTC(), Volume: 10, OrderCount: 1}}}}
	history := poller.NewHistory(gateway, database)
	if err := history.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	gateway.err = errors.New("ESI unavailable")
	if err := history.Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want fetch failure")
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM market_history`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("market_history count = %d, want previous window preserved", count)
	}
}
