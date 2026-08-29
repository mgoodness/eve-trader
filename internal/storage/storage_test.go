package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenRejectsPathContainingQuestionMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eve-trader?.db")

	if _, err := Open(path); err == nil {
		t.Fatal("Open: want error for a path containing '?', got nil")
	}
}

func TestBootstrapCreatesSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Bootstrap must be idempotent: safe to call again against an
	// already-bootstrapped file (e.g. every startup).
	if err := store.Bootstrap(ctx); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}

	var name string
	row := store.DB.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'oauth_token'`)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("oauth_token table not found: %v", err)
	}
}

func TestPing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenEnablesWALJournalMode(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	var mode string
	if err := store.DB.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenConfiguresBusyTimeout(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	var timeoutMs int
	if err := store.DB.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeoutMs); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if timeoutMs != busyTimeoutMS {
		t.Errorf("busy_timeout = %dms, want %dms", timeoutMs, busyTimeoutMS)
	}
}

// TestConcurrentWritersDoNotProduceSQLiteBusy is the AC-mandated
// regression test: many goroutines writing to the same temp-file
// database concurrently must not surface SQLITE_BUSY ("database is
// locked") errors, which they reliably did before WAL mode + busy_timeout
// were configured (see issue #17's investigation and issue #23).
func TestConcurrentWritersDoNotProduceSQLiteBusy(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	const concurrency = 20
	now := time.Now()

	var wg sync.WaitGroup
	errs := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := store.InsertAlertFeedEntry(ctx, AlertFeedEntry{
				OrderID:    int64(i),
				AlertType:  "undercut",
				TypeID:     34,
				LocationID: 60003760,
				Detail:     fmt.Sprintf("entry-%d", i),
				CreatedAt:  now.Add(time.Duration(i) * time.Millisecond),
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("InsertAlertFeedEntry: %v", err)
	}

	got, err := store.RecentAlertFeed(ctx, concurrency+10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(got) != concurrency {
		t.Fatalf("feed entries = %d, want %d", len(got), concurrency)
	}
}

func TestLoadTokenBeforeSaveReturnsErrNoToken(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	_, err := store.LoadToken(ctx)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("LoadToken error = %v, want ErrNoToken", err)
	}
}

func TestSaveTokenThenLoadTokenRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	want := Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(20 * time.Minute).UTC().Truncate(time.Second),
	}
	if err := store.SaveToken(ctx, want); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestSaveTokenOverwritesPreviousToken(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	first := Token{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(20 * time.Minute)}
	second := Token{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(40 * time.Minute)}

	if err := store.SaveToken(ctx, first); err != nil {
		t.Fatalf("SaveToken(first): %v", err)
	}
	if err := store.SaveToken(ctx, second); err != nil {
		t.Fatalf("SaveToken(second): %v", err)
	}

	got, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.RefreshToken != second.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q (the newer token)", got.RefreshToken, second.RefreshToken)
	}

	var rowCount int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_token`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("oauth_token row count = %d, want 1 (single-row table)", rowCount)
	}
}

func TestGetOrderAlertStateWhenNoneExists(t *testing.T) {
	store := openBootstrapped(t)

	_, exists, err := store.GetOrderAlertState(context.Background(), 1, "undercut")
	if err != nil {
		t.Fatalf("GetOrderAlertState: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

func TestUpsertThenGetOrderAlertState(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	firedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertOrderAlertState(ctx, 1, "undercut", firedAt); err != nil {
		t.Fatalf("UpsertOrderAlertState: %v", err)
	}

	got, exists, err := store.GetOrderAlertState(ctx, 1, "undercut")
	if err != nil {
		t.Fatalf("GetOrderAlertState: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	if !got.Equal(firedAt) {
		t.Errorf("LastFiredAt = %v, want %v", got, firedAt)
	}
}

func TestUpsertOrderAlertStateOverwritesLastFiredAt(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	first := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	second := time.Now().UTC().Truncate(time.Second)

	if err := store.UpsertOrderAlertState(ctx, 1, "undercut", first); err != nil {
		t.Fatalf("UpsertOrderAlertState(first): %v", err)
	}
	if err := store.UpsertOrderAlertState(ctx, 1, "undercut", second); err != nil {
		t.Fatalf("UpsertOrderAlertState(second): %v", err)
	}

	got, exists, err := store.GetOrderAlertState(ctx, 1, "undercut")
	if err != nil {
		t.Fatalf("GetOrderAlertState: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	if !got.Equal(second) {
		t.Errorf("LastFiredAt = %v, want %v (the newer value)", got, second)
	}
}

func TestOrderAlertStateIsScopedPerOrderAndType(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	firedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertOrderAlertState(ctx, 1, "undercut", firedAt); err != nil {
		t.Fatalf("UpsertOrderAlertState: %v", err)
	}

	if _, exists, err := store.GetOrderAlertState(ctx, 2, "undercut"); err != nil {
		t.Fatalf("GetOrderAlertState(other order): %v", err)
	} else if exists {
		t.Error("state leaked to a different order ID")
	}
	if _, exists, err := store.GetOrderAlertState(ctx, 1, "expiring"); err != nil {
		t.Fatalf("GetOrderAlertState(other type): %v", err)
	} else if exists {
		t.Error("state leaked to a different alert type")
	}
}

func TestClearOrderAlertState(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	if err := store.UpsertOrderAlertState(ctx, 1, "undercut", time.Now()); err != nil {
		t.Fatalf("UpsertOrderAlertState: %v", err)
	}
	if err := store.ClearOrderAlertState(ctx, 1, "undercut"); err != nil {
		t.Fatalf("ClearOrderAlertState: %v", err)
	}

	_, exists, err := store.GetOrderAlertState(ctx, 1, "undercut")
	if err != nil {
		t.Fatalf("GetOrderAlertState: %v", err)
	}
	if exists {
		t.Error("exists = true after ClearOrderAlertState, want false")
	}
}

func TestClearOrderAlertStateWhenNoneExistsIsANoop(t *testing.T) {
	store := openBootstrapped(t)

	if err := store.ClearOrderAlertState(context.Background(), 1, "undercut"); err != nil {
		t.Fatalf("ClearOrderAlertState: %v", err)
	}
}

func TestInsertAndListRecentAlertFeed(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	base := time.Now().UTC().Truncate(time.Second)
	entries := []AlertFeedEntry{
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "oldest", CreatedAt: base.Add(-2 * time.Hour)},
		{OrderID: 2, AlertType: "expiring", TypeID: 35, LocationID: 60003760, Detail: "middle", CreatedAt: base.Add(-1 * time.Hour)},
		{OrderID: 3, AlertType: "price_moved", TypeID: 36, LocationID: 60004588, Detail: "newest", CreatedAt: base},
	}
	for _, e := range entries {
		if err := store.InsertAlertFeedEntry(ctx, e); err != nil {
			t.Fatalf("InsertAlertFeedEntry: %v", err)
		}
	}

	got, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	// Most recent first.
	if got[0].Detail != "newest" || got[1].Detail != "middle" || got[2].Detail != "oldest" {
		t.Errorf("order = [%s %s %s], want [newest middle oldest]", got[0].Detail, got[1].Detail, got[2].Detail)
	}
	if got[0].OrderID != 3 || got[0].AlertType != "price_moved" || got[0].TypeID != 36 || got[0].LocationID != 60004588 {
		t.Errorf("got[0] = %+v, unexpected fields", got[0])
	}
}

// TestRecentAlertFeedCollapsesRepeatedOrderAlertTypeToLatest is the
// AC-mandated test: an Order+Alert-type that fired repeatedly (e.g. a
// still-active condition re-firing every 4h past its throttle window)
// shows only its single most recent firing in the displayed feed, not
// every historical entry.
func TestRecentAlertFeedCollapsesRepeatedOrderAlertTypeToLatest(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	base := time.Now().UTC().Truncate(time.Second)
	entries := []AlertFeedEntry{
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "first firing", CreatedAt: base.Add(-8 * time.Hour)},
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "second firing", CreatedAt: base.Add(-4 * time.Hour)},
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "third firing", CreatedAt: base},
	}
	for _, e := range entries {
		if err := store.InsertAlertFeedEntry(ctx, e); err != nil {
			t.Fatalf("InsertAlertFeedEntry: %v", err)
		}
	}

	got, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (repeated Order+Alert-type firings collapse to the latest)", len(got))
	}
	if got[0].Detail != "third firing" {
		t.Errorf("got[0].Detail = %q, want %q (the most recent firing)", got[0].Detail, "third firing")
	}
}

// TestRecentAlertFeedKeepsDifferentAlertTypesForSameOrderIndependent
// proves deduplication is scoped to (Order, Alert type), not to the
// Order alone -- an Order with two distinct active conditions still
// shows both.
func TestRecentAlertFeedKeepsDifferentAlertTypesForSameOrderIndependent(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	base := time.Now().UTC().Truncate(time.Second)
	entries := []AlertFeedEntry{
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "undercut firing", CreatedAt: base.Add(-1 * time.Hour)},
		{OrderID: 1, AlertType: "price_moved", TypeID: 34, LocationID: 60003760, Detail: "price-moved firing", CreatedAt: base},
	}
	for _, e := range entries {
		if err := store.InsertAlertFeedEntry(ctx, e); err != nil {
			t.Fatalf("InsertAlertFeedEntry: %v", err)
		}
	}

	got, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (different Alert types on the same Order must stay independent)", len(got))
	}
	if got[0].Detail != "price-moved firing" || got[1].Detail != "undercut firing" {
		t.Errorf("got = [%s %s], want [price-moved firing, undercut firing] (most recent first)", got[0].Detail, got[1].Detail)
	}
}

func TestRecentAlertFeedRespectsLimit(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		err := store.InsertAlertFeedEntry(ctx, AlertFeedEntry{
			OrderID: int64(i), AlertType: "undercut", TypeID: 34, LocationID: 60003760,
			Detail: fmt.Sprintf("entry-%d", i), CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("InsertAlertFeedEntry: %v", err)
		}
	}

	got, err := store.RecentAlertFeed(ctx, 2)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Detail != "entry-4" || got[1].Detail != "entry-3" {
		t.Errorf("got = [%s %s], want [entry-4 entry-3]", got[0].Detail, got[1].Detail)
	}
}

func openBootstrapped(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return store
}
