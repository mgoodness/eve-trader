package poller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

const (
	// HistoryRetention is the number of calendar days retained in market_history.
	HistoryRetention = 14
	// HistoryResetHour and HistoryResetMinute are ESI's approximate daily cache
	// reset time. Refreshes are scheduled just after this point.
	HistoryResetHour   = 11
	HistoryResetMinute = 5

	// historyConcurrency is the fixed number of workers fetching history at
	// once. See ADR 0002 for why it is fixed rather than adaptive.
	historyConcurrency = 12
	// historyCommitBatch is how many successful types are accumulated before a
	// transaction commits them, so an interrupted refresh keeps the progress
	// already written.
	historyCommitBatch = 500
	// historyRequestTimeout bounds a single ESI history request.
	historyRequestTimeout = 15 * time.Second
	// historyRefreshTimeout bounds a whole refresh (30 minutes). On expiry the
	// pool stops dispatching, in-flight requests finish, and the remainder is
	// committed.
	historyRefreshTimeout = 30 * time.Minute
	// historyMaxAttempts is the total number of tries per type, retries included.
	historyMaxAttempts = 3
	// historyProgressEvery logs progress at debug level every N refreshed types.
	historyProgressEvery = 1000
)

// historyBackoff is the delay before the next attempt (indexed by attempt-1)
// when ESI gave no Retry-After. Jitter is applied on top.
var historyBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// HistoryPoller refreshes the rolling market history window for every type
// currently present in market_order. A failed refresh leaves the prior window
// intact.
type HistoryPoller struct {
	gateway esi.ESIGateway
	db      *sql.DB
	mu      sync.Mutex

	// requestTimeout, refreshTimeout and backoff default to the constants
	// above; tests override them to run quickly.
	requestTimeout time.Duration
	refreshTimeout time.Duration
	backoff        []time.Duration
}

func NewHistory(gateway esi.ESIGateway, db *sql.DB) *HistoryPoller {
	return &HistoryPoller{
		gateway:        gateway,
		db:             db,
		requestTimeout: historyRequestTimeout,
		refreshTimeout: historyRefreshTimeout,
		backoff:        historyBackoff,
	}
}

// Poll fetches history for each distinct type on the current order book and
// writes it in chunks. Workers fetch concurrently under a fixed pool, but a
// type whose fetch fails is skipped: its previous window is left intact and
// the remaining types are still refreshed, so one bad type (e.g. a
// non-marketable item ESI 404s on) cannot discard the whole batch. Poll
// returns an error only when it refreshed nothing.
func (p *HistoryPoller) Poll(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	rows, err := p.db.QueryContext(ctx, `SELECT DISTINCT type_id FROM market_order ORDER BY type_id`)
	if err != nil {
		return fmt.Errorf("finding market-history type IDs: %w", err)
	}
	var typeIDs []int
	for rows.Next() {
		var typeID int
		if err := rows.Scan(&typeID); err != nil {
			rows.Close()
			return fmt.Errorf("scanning market-history type ID: %w", err)
		}
		typeIDs = append(typeIDs, typeID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading market-history type IDs: %w", err)
	}
	rows.Close()
	if len(typeIDs) == 0 {
		return nil
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	cutoff := today.AddDate(0, 0, -(HistoryRetention - 1))
	started := time.Now()
	slog.Info("refreshing market history", "types", len(typeIDs), "concurrency", historyConcurrency)

	// The ceiling bounds dispatch only: in-flight requests are allowed to
	// finish (each carries its own per-request timeout) so the pool can commit
	// the work it already did.
	dispatchCtx, cancelDispatch := context.WithTimeout(ctx, p.refreshTimeout)
	defer cancelDispatch()

	gate := &rateLimitGate{}
	jobs := make(chan int)
	results := make(chan historyResult, historyConcurrency)

	var wg sync.WaitGroup
	for range historyConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.worker(ctx, gate, jobs, results)
		}()
	}
	go func() {
		defer close(jobs)
		for _, typeID := range typeIDs {
			select {
			case jobs <- typeID:
			case <-dispatchCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		refreshed int
		failed    int
		attempted int
		pending   = make(map[int][]esi.HistoryPoint)
		commitErr error
	)
	for result := range results {
		attempted++
		if result.err != nil {
			failed++
			continue
		}
		refreshed++
		if commitErr != nil {
			continue
		}
		pending[result.typeID] = result.points
		if refreshed%historyProgressEvery == 0 {
			slog.Debug("market-history refresh progress", "refreshed", refreshed, "of", len(typeIDs))
		}
		if len(pending) >= historyCommitBatch {
			if err := p.writeHistory(ctx, pending, cutoff, today); err != nil {
				commitErr = err
				cancelDispatch()
				continue
			}
			pending = make(map[int][]esi.HistoryPoint)
		}
	}
	// Only commit the remainder on a clean run; a cancelled caller is shutting
	// down and the chunks already written are what matters.
	if commitErr == nil && ctx.Err() == nil && len(pending) > 0 {
		commitErr = p.writeHistory(ctx, pending, cutoff, today)
	}

	timedOut := ctx.Err() == nil && errors.Is(dispatchCtx.Err(), context.DeadlineExceeded)
	if failed > 0 {
		slog.Warn("market-history types failed to refresh", "failed", failed, "of", len(typeIDs))
	}
	skipped := len(typeIDs) - attempted
	slog.Info("market-history refresh complete",
		"refreshed", refreshed, "skipped", skipped, "failed", failed,
		"elapsed", time.Since(started).Round(time.Second), "timed_out", timedOut)

	if commitErr != nil {
		return fmt.Errorf("writing market history: %w", commitErr)
	}
	if refreshed == 0 {
		return fmt.Errorf("refreshing market history: no types refreshed of %d", len(typeIDs))
	}
	return nil
}

// historyResult is one worker's outcome for a single type.
type historyResult struct {
	typeID int
	points []esi.HistoryPoint
	err    error
}

// worker pulls type IDs and fetches each one until the job channel is closed.
func (p *HistoryPoller) worker(ctx context.Context, gate *rateLimitGate, jobs <-chan int, results chan<- historyResult) {
	for typeID := range jobs {
		points, err := p.fetchHistory(ctx, gate, typeID)
		select {
		case results <- historyResult{typeID: typeID, points: points, err: err}:
		case <-ctx.Done():
			return
		}
	}
}

// fetchHistory fetches one type, retrying retryable failures up to
// historyMaxAttempts. A rate-limited response pauses the whole pool until
// Retry-After via gate; other retryable failures back off locally. A 404 (or
// any other 4xx) is not retried.
func (p *HistoryPoller) fetchHistory(ctx context.Context, gate *rateLimitGate, typeID int) ([]esi.HistoryPoint, error) {
	var lastErr error
	for attempt := 1; attempt <= historyMaxAttempts; attempt++ {
		if err := gate.wait(ctx); err != nil {
			return nil, err
		}
		reqCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
		points, err := p.gateway.FetchHistory(reqCtx, typeID)
		cancel()
		if err == nil {
			return points, nil
		}
		lastErr = err

		var rateLimited *esi.RateLimited
		if errors.As(err, &rateLimited) {
			delay := rateLimited.RetryAfter
			if delay <= 0 {
				delay = p.backoffDelay(attempt)
			}
			gate.pause(delay)
			continue
		}
		if !retryable(err) {
			return nil, err
		}
		if attempt < historyMaxAttempts {
			if err := sleep(ctx, p.jitter(p.backoffDelay(attempt))); err != nil {
				return nil, err
			}
		}
	}
	return nil, lastErr
}

// retryable reports whether an error is worth another attempt. ESI 5xx and
// other HTTP errors (notably 404) are classified by status; anything else --
// rate limiting and network errors -- retries.
func retryable(err error) bool {
	var httpErr *esi.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	return true
}

func (p *HistoryPoller) backoffDelay(attempt int) time.Duration {
	if len(p.backoff) == 0 {
		return 0
	}
	i := attempt - 1
	if i >= len(p.backoff) {
		i = len(p.backoff) - 1
	}
	return p.backoff[i]
}

// jitter spreads a backoff delay over [d/2, d] to avoid a thundering herd.
func (*HistoryPoller) jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + rand.N(half+1)
}

// writeHistory applies one chunk of refreshed windows in its own transaction.
// Each type's retained slice is replaced (so a day ESI omitted is not kept
// from an earlier refresh) and rows older than the window are pruned.
func (p *HistoryPoller) writeHistory(ctx context.Context, history map[int][]esi.HistoryPoint, cutoff, today time.Time) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting history poll transaction: %w", err)
	}
	defer tx.Rollback()
	cutoffText := cutoff.Format("2006-01-02")
	for typeID, points := range history {
		if _, err := tx.ExecContext(ctx, `DELETE FROM market_history WHERE type_id = ? AND date >= ?`, typeID, cutoffText); err != nil {
			return fmt.Errorf("resetting history for type %d: %w", typeID, err)
		}
		for _, point := range points {
			date := point.Date.UTC().Format("2006-01-02")
			if point.Date.UTC().Before(cutoff) || date > today.Format("2006-01-02") {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO market_history (type_id, date, volume, order_count) VALUES (?, ?, ?, ?)
				ON CONFLICT(type_id, date) DO UPDATE SET volume=excluded.volume, order_count=excluded.order_count`,
				typeID, date, point.Volume, point.OrderCount); err != nil {
				return fmt.Errorf("upserting history for type %d on %s: %w", typeID, date, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM market_history WHERE date < ?`, cutoffText); err != nil {
		return fmt.Errorf("pruning market history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing history poll: %w", err)
	}
	return nil
}

// rateLimitGate coordinates a pool-wide pause after ESI reports rate
// limiting: the first worker to see a 420/429 pushes the resume time out, and
// every worker waits for it before its next fetch.
type rateLimitGate struct {
	mu    sync.Mutex
	until time.Time
}

func (g *rateLimitGate) pause(d time.Duration) {
	if d <= 0 {
		return
	}
	g.mu.Lock()
	if end := time.Now().Add(d); end.After(g.until) {
		g.until = end
	}
	g.mu.Unlock()
}

func (g *rateLimitGate) wait(ctx context.Context) error {
	g.mu.Lock()
	until := g.until
	g.mu.Unlock()
	return sleep(ctx, time.Until(until))
}

// sleep waits for d or ctx cancellation, whichever comes first.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// historyStartupRetry is how long Run waits between checks for a populated
// order book before its first refresh.
const historyStartupRetry = 10 * time.Second

// waitForOrderBook blocks until market_order has at least one row or ctx is
// done, re-checking every retry. It returns true once the book is populated
// and false if ctx was cancelled first.
func waitForOrderBook(ctx context.Context, db *sql.DB, retry time.Duration) bool {
	for {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_order`).Scan(&n); err == nil && n > 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(retry):
		}
	}
}

// Run performs an immediate refresh and then refreshes daily. It is intended
// to be started as a goroutine alongside the order-book poller.
func (p *HistoryPoller) Run(ctx context.Context) {
	// The type set is derived from market_order, which the order poller fills
	// on its own schedule. Wait for it so a first boot doesn't no-op here and
	// then sit idle until the next daily reset.
	if !waitForOrderBook(ctx, p.db, historyStartupRetry) {
		return
	}
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling market history", "err", err)
	}
	timer := time.NewTimer(time.Until(nextHistoryRefresh(time.Now().UTC())))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling market history", "err", err)
			}
			timer.Reset(time.Until(nextHistoryRefresh(time.Now().UTC())))
		}
	}
}

func nextHistoryRefresh(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), HistoryResetHour, HistoryResetMinute+1, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
