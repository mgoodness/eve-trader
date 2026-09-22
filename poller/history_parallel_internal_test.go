package poller

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

// captureHandler records every log record for assertions.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) find(message string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == message {
			return r, true
		}
	}
	return slog.Record{}, false
}

func attrValue(r slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	ok := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

// trackingGateway is an ESIGateway whose history behavior is supplied per
// test. The embedded Fake covers the methods these tests never call.
type trackingGateway struct {
	esi.Fake
	fetch func(ctx context.Context, typeID int) ([]esi.HistoryPoint, error)
}

func (g *trackingGateway) FetchHistory(ctx context.Context, typeID int) ([]esi.HistoryPoint, error) {
	return g.fetch(ctx, typeID)
}

// seedOrderBook inserts n item_type and market_order rows, giving the
// history poller n distinct types to refresh.
func seedOrderBook(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := tx.Exec(`INSERT INTO item_type (type_id, name, updated_at) VALUES (?, ?, '2024-01-01T00:00:00Z')`, i, "Item"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO market_order (order_id, type_id, location_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
			VALUES (?, ?, 60004588, 1, 5, 1000, 1000, 1, '2024-01-01T00:00:00Z', 90, '2024-01-01T00:00:00Z')`, i, i); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func countHistory(db *sql.DB) int {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM market_history`).Scan(&n); err != nil {
		return -1
	}
	return n
}

// TestHistoryPollBoundsConcurrency asserts the pool never exceeds
// historyConcurrency simultaneous in-flight fetches, and actually runs
// fetches in parallel.
func TestHistoryPollBoundsConcurrency(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, 200)

	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	gateway := &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()

		time.Sleep(2 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
		return []esi.HistoryPoint{{Date: time.Now().UTC(), Volume: 1}}, nil
	}}

	if err := NewHistory(gateway, db).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if maxInFlight > historyConcurrency {
		t.Fatalf("max in-flight fetches = %d, want at most %d", maxInFlight, historyConcurrency)
	}
	if maxInFlight < 2 {
		t.Fatalf("max in-flight fetches = %d, want concurrent fetches", maxInFlight)
	}
	if got := countHistory(db); got != 200 {
		t.Fatalf("market_history rows = %d, want 200", got)
	}
}

// TestHistoryPollCommitsProgressInChunks interrupts a refresh partway and
// asserts the already-committed chunk survives: an all-at-once write would
// leave the table empty.
func TestHistoryPollCommitsProgressInChunks(t *testing.T) {
	db := openFileDB(t)
	const types = historyCommitBatch + 100
	seedOrderBook(t, db, types)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gateway := &trackingGateway{fetch: func(ctx context.Context, typeID int) ([]esi.HistoryPoint, error) {
		if typeID > historyCommitBatch {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []esi.HistoryPoint{{Date: time.Now().UTC(), Volume: 1}}, nil
	}}

	done := make(chan error, 1)
	go func() { done <- NewHistory(gateway, db).Poll(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for countHistory(db) < historyCommitBatch {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the first committed chunk")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if got := countHistory(db); got != historyCommitBatch {
		t.Fatalf("market_history rows after interruption = %d, want the committed chunk of %d", got, historyCommitBatch)
	}
}

// TestHistoryPollRetriesRetryableFailures asserts a 5xx is retried up to
// historyMaxAttempts and then counted as a failed type.
func TestHistoryPollRetriesRetryableFailures(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, 1)

	p := NewHistory(nil, db)
	p.backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	attempts := 0
	p.gateway = &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		attempts++
		return nil, &esi.HTTPError{StatusCode: 503, Status: "503 Service Unavailable"}
	}}

	if err := p.Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want an error when nothing refreshed")
	}
	if attempts != historyMaxAttempts {
		t.Fatalf("fetch attempts = %d, want %d", attempts, historyMaxAttempts)
	}
}

// TestHistoryPollDoesNotRetryNotFound asserts a 404 is a permanent per-type
// failure: fetched once, skipped, prior window untouched.
func TestHistoryPollDoesNotRetryNotFound(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, 1)

	p := NewHistory(nil, db)
	p.backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	attempts := 0
	p.gateway = &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		attempts++
		return nil, &esi.HTTPError{StatusCode: 404, Status: "404 Not Found"}
	}}

	if err := p.Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want an error when nothing refreshed")
	}
	if attempts != 1 {
		t.Fatalf("fetch attempts = %d, want 1 (404 is not retried)", attempts)
	}
}

// TestHistoryPollRateLimitedIsRetriedNotFailed asserts a 420 with Retry-After
// pauses the pool but is retried, so the type is refreshed rather than
// permanently dropped.
func TestHistoryPollRateLimitedIsRetriedNotFailed(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, 1)

	attempts := 0
	gateway := &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		attempts++
		if attempts == 1 {
			return nil, &esi.RateLimited{RetryAfter: 5 * time.Millisecond}
		}
		return []esi.HistoryPoint{{Date: time.Now().UTC(), Volume: 7}}, nil
	}}

	if err := NewHistory(gateway, db).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("fetch attempts = %d, want the rate-limited type retried once", attempts)
	}
	if got := countHistory(db); got != 1 {
		t.Fatalf("market_history rows = %d, want the type refreshed", got)
	}
}

// TestRateLimitGateWaitsAndExtends covers the pool-wide pause directly.
func TestRateLimitGateWaitsAndExtends(t *testing.T) {
	var gate rateLimitGate
	gate.pause(30 * time.Millisecond)
	gate.pause(10 * time.Millisecond) // must not shorten the pending pause

	start := time.Now()
	if err := gate.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("gate waited %v, want at least 30ms", elapsed)
	}
}

// TestHistoryPollLogsProgressAndSummary asserts the debug progress line and
// the completion summary (with its timed_out flag).
func TestHistoryPollLogsProgressAndSummary(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, historyProgressEvery+1)

	handler := &captureHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(previous)

	gateway := &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		return []esi.HistoryPoint{{Date: time.Now().UTC(), Volume: 1}}, nil
	}}
	if err := NewHistory(gateway, db).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, ok := handler.find("market-history refresh progress"); !ok {
		t.Fatal("no progress log line emitted")
	}
	summary, ok := handler.find("market-history refresh complete")
	if !ok {
		t.Fatal("no completion summary emitted")
	}
	if v, _ := attrValue(summary, "refreshed"); v.Int64() != historyProgressEvery+1 {
		t.Fatalf("summary refreshed = %s, want %d", v, historyProgressEvery+1)
	}
	if v, _ := attrValue(summary, "failed"); v.Int64() != 0 {
		t.Fatalf("summary failed = %s, want 0", v)
	}
	if v, _ := attrValue(summary, "timed_out"); v.Bool() {
		t.Fatal("summary timed_out = true, want false")
	}
}

// TestHistoryPollStopsAtCeilingAndCommitsRemainder asserts the whole-refresh
// ceiling stops dispatch, lets in-flight requests finish, commits the
// remainder, and does not error when at least one type refreshed.
func TestHistoryPollStopsAtCeilingAndCommitsRemainder(t *testing.T) {
	db := openFileDB(t)
	seedOrderBook(t, db, 30)

	handler := &captureHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(previous)

	p := NewHistory(nil, db)
	p.refreshTimeout = 40 * time.Millisecond
	p.requestTimeout = time.Second
	p.gateway = &trackingGateway{fetch: func(context.Context, int) ([]esi.HistoryPoint, error) {
		// Every fetch outlives the ceiling, so only the initially-dispatched
		// workers complete.
		time.Sleep(60 * time.Millisecond)
		return []esi.HistoryPoint{{Date: time.Now().UTC(), Volume: 1}}, nil
	}}

	if err := p.Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want nil when some types refreshed", err)
	}
	if got := countHistory(db); got != historyConcurrency {
		t.Fatalf("market_history rows = %d, want the %d in-flight types committed", got, historyConcurrency)
	}
	summary, ok := handler.find("market-history refresh complete")
	if !ok {
		t.Fatal("no completion summary emitted")
	}
	if v, _ := attrValue(summary, "timed_out"); !v.Bool() {
		t.Fatalf("summary timed_out = %s, want true", v)
	}
}
