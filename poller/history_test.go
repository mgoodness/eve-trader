package poller_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/poller"
	"github.com/mgoodness/eve-trader/ranking"
)

type historyGateway struct {
	orders  []esi.Order
	history map[int][]esi.HistoryPoint
	fail    map[int]error

	mu    sync.Mutex
	calls []int
}

func (g *historyGateway) FetchRensOrders(context.Context) ([]esi.Order, error) { return g.orders, nil }
func (*historyGateway) FetchTypeNames(context.Context, []int) (map[int]string, error) {
	return nil, nil
}
func (g *historyGateway) FetchHistory(_ context.Context, typeID int) ([]esi.HistoryPoint, error) {
	g.mu.Lock()
	g.calls = append(g.calls, typeID)
	g.mu.Unlock()
	if err := g.fail[typeID]; err != nil {
		return nil, err
	}
	return g.history[typeID], nil
}
func (*historyGateway) FetchCharacterSkills(context.Context, int, string) (esi.Skills, error) {
	return esi.Skills{}, nil
}
func (*historyGateway) ExchangeCode(context.Context, string, string) (esi.Token, error) {
	return esi.Token{}, nil
}
func (*historyGateway) RefreshToken(context.Context, string) (esi.Token, error) {
	return esi.Token{}, nil
}

func TestHistoryPollStoresRollingWindowAndRankingUsesAverage(t *testing.T) {
	database := dbtest.OpenDB(t)
	issued := time.Now().UTC()
	gateway := &historyGateway{
		orders: []esi.Order{
			{OrderID: 1, TypeID: 34, IsBuyOrder: true, Price: 5, Issued: issued},
			{OrderID: 2, TypeID: 34, Price: 8, Issued: issued},
			// Second order inside the near-best band on each side, so the
			// seeded book is not a single-order spread.
			{OrderID: 3, TypeID: 34, IsBuyOrder: true, Price: 4.9, Issued: issued},
			{OrderID: 4, TypeID: 34, Price: 8.4, Issued: issued},
		},
		history: map[int][]esi.HistoryPoint{34: {
			{Date: time.Now().UTC(), Volume: 10, OrderCount: 1, Average: 1.5, Highest: 2, Lowest: 1},
			{Date: time.Now().UTC().AddDate(0, 0, -1), Volume: 20, OrderCount: 2, Average: 3.5, Highest: 4, Lowest: 3},
			{Date: time.Now().UTC().AddDate(0, 0, -2), Volume: 15, OrderCount: 3, Average: 2, Highest: 2, Lowest: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -3), Volume: 15, OrderCount: 3, Average: 2, Highest: 2, Lowest: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -4), Volume: 15, OrderCount: 3, Average: 2, Highest: 2, Lowest: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -5), Volume: 15, OrderCount: 3, Average: 2, Highest: 2, Lowest: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -6), Volume: 15, OrderCount: 3, Average: 2, Highest: 2, Lowest: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -30), Volume: 999, OrderCount: 9, Average: 9, Highest: 9, Lowest: 9},
		}},
	}
	orders := poller.New(gateway, database, time.Hour)
	if err := orders.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := poller.NewHistory(gateway, database).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM market_history`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("history row count = %d, want 7", count)
	}
	var volume float64
	if err := database.QueryRow(`SELECT AVG(volume) FROM market_history WHERE type_id = 34`).Scan(&volume); err != nil {
		t.Fatal(err)
	}
	if volume != 15 {
		t.Fatalf("average volume = %v, want 15", volume)
	}
	var avg, high, low float64
	if err := database.QueryRow(
		`SELECT average, highest, lowest FROM market_history WHERE type_id = 34 AND date = ?`,
		time.Now().UTC().Format("2006-01-02"),
	).Scan(&avg, &high, &low); err != nil {
		t.Fatal(err)
	}
	if avg != 1.5 || high != 2 || low != 1 {
		t.Fatalf("stored prices = average %v highest %v lowest %v, want 1.5/2/1", avg, high, low)
	}
	got, err := ranking.Load(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Opportunities) != 1 || got.Opportunities[0].VolumePerDay != 15 {
		t.Fatalf("ranking volume = %+v, want one opportunity at 15", got.Opportunities)
	}
	if len(gateway.calls) != 1 || gateway.calls[0] != 34 {
		t.Fatalf("FetchHistory calls = %v, want [34]", gateway.calls)
	}
}

func TestHistoryPollSkipsFailedTypeAndRefreshesTheRest(t *testing.T) {
	database := dbtest.OpenDB(t)
	issued := time.Now().UTC()
	gateway := &historyGateway{
		orders: []esi.Order{
			{OrderID: 1, TypeID: 34, Issued: issued},
			{OrderID: 2, TypeID: 35, Issued: issued},
		},
		history: map[int][]esi.HistoryPoint{
			34: {{Date: time.Now().UTC(), Volume: 10}},
			35: {{Date: time.Now().UTC(), Volume: 20}},
		},
	}
	if err := poller.New(gateway, database, time.Hour).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	history := poller.NewHistory(gateway, database)
	if err := history.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	// One type now 404s (a non-marketable item); the other gets a new volume.
	gateway.fail = map[int]error{35: &esi.HTTPError{StatusCode: http.StatusNotFound, Status: "404 Not Found"}}
	gateway.history = map[int][]esi.HistoryPoint{34: {{Date: time.Now().UTC(), Volume: 99}}}
	if err := history.Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want a partial refresh to succeed", err)
	}

	var v34, v35 float64
	if err := database.QueryRow(`SELECT AVG(volume) FROM market_history WHERE type_id = 34`).Scan(&v34); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT AVG(volume) FROM market_history WHERE type_id = 35`).Scan(&v35); err != nil {
		t.Fatal(err)
	}
	if v34 != 99 {
		t.Fatalf("type 34 volume = %v, want the successful type refreshed to 99", v34)
	}
	if v35 != 20 {
		t.Fatalf("type 35 volume = %v, want the failed type's previous window preserved", v35)
	}
}

func TestHistoryPollKeepsExactlyThirtyCalendarDays(t *testing.T) {
	database := dbtest.OpenDB(t)
	issued := time.Now().UTC()
	gateway := &historyGateway{
		orders: []esi.Order{{OrderID: 1, TypeID: 34, Issued: issued}},
		history: map[int][]esi.HistoryPoint{34: {
			{Date: time.Now().UTC(), Volume: 1},
			{Date: time.Now().UTC().AddDate(0, 0, -29), Volume: 2},
			{Date: time.Now().UTC().AddDate(0, 0, -30), Volume: 3},
		}},
	}
	if err := poller.New(gateway, database, time.Hour).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := poller.NewHistory(gateway, database).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM market_history`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("history row count = %d, want 2", count)
	}
}
