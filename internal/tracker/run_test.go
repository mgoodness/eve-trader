package tracker

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

func openStore(t *testing.T) *storage.Store {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return store
}

func TestFetchSnapshotFindsBestCompetingPrice(t *testing.T) {
	fake := esi.NewFakeClient()
	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)
	order.OrderID = 999 // distinct from any canned MarketOrder

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 100, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.60},
		{OrderID: 101, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.45}, // best (lowest)
		{OrderID: 102, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.55},
		{OrderID: 103, TypeID: 99999, LocationID: order.LocationID, IsBuyOrder: false, Price: 0.01}, // different item, ignored
		{OrderID: 104, TypeID: order.TypeID, LocationID: 60004588, IsBuyOrder: false, Price: 0.01},  // different hub, ignored
	}

	snap, err := FetchSnapshot(context.Background(), fake, order)
	if err != nil {
		t.Fatalf("FetchSnapshot: %v", err)
	}
	if !snap.HasCompetition {
		t.Fatal("HasCompetition = false, want true")
	}
	if snap.BestCompetingPrice != 5.45 {
		t.Errorf("BestCompetingPrice = %v, want 5.45", snap.BestCompetingPrice)
	}
}

func TestFetchSnapshotExcludesTheOrderItself(t *testing.T) {
	fake := esi.NewFakeClient()
	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)
	order.OrderID = 500

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: order.OrderID, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: order.Price},
	}

	snap, err := FetchSnapshot(context.Background(), fake, order)
	if err != nil {
		t.Fatalf("FetchSnapshot: %v", err)
	}
	if snap.HasCompetition {
		t.Errorf("HasCompetition = true, want false (the only matching entry is the order itself)")
	}
}

func TestFetchSnapshotNoCompetition(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = nil
	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)

	snap, err := FetchSnapshot(context.Background(), fake, order)
	if err != nil {
		t.Fatalf("FetchSnapshot: %v", err)
	}
	if snap.HasCompetition {
		t.Error("HasCompetition = true, want false")
	}
}

func TestFetchSnapshotPropagatesError(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.MarketOrdersErr = context.DeadlineExceeded
	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)

	if _, err := FetchSnapshot(context.Background(), fake, order); err == nil {
		t.Fatal("FetchSnapshot: want error, got nil")
	}
}

// TestRunFiresOnNewDetectionThenSuppressesRepeats is the AC-mandated test
// for throttling/suppression across repeated evaluation cycles: an Alert
// fires once on new detection, then suppresses repeats for that
// Order+Alert-type for 4 hours or until the condition resolves.
func TestRunFiresOnNewDetectionThenSuppressesRepeats(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()

	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)
	order.OrderID = 42
	orders := []esi.Order{order}

	// Cycle 1: undercut. New detection -- should fire.
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.40},
	}
	cycle1 := time.Now()
	results, err := Run(ctx, fake, store, orders, cycle1)
	if err != nil {
		t.Fatalf("Run (cycle 1): %v", err)
	}
	if !has(results[0].Alerts, Undercut) {
		t.Fatalf("cycle 1: Undercut not active, got %+v", results[0].Alerts)
	}
	feed, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 1 {
		t.Fatalf("feed entries after cycle 1 = %d, want 1 (new detection fires)", len(feed))
	}

	// Cycle 2: condition still true, well within the 4h throttle window --
	// should still report as active (for the badge) but NOT add a new feed
	// entry.
	cycle2 := cycle1.Add(1 * time.Hour)
	results, err = Run(ctx, fake, store, orders, cycle2)
	if err != nil {
		t.Fatalf("Run (cycle 2): %v", err)
	}
	if !has(results[0].Alerts, Undercut) {
		t.Fatalf("cycle 2: Undercut not active, got %+v", results[0].Alerts)
	}
	feed, err = store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 1 {
		t.Fatalf("feed entries after cycle 2 = %d, want 1 (still suppressed)", len(feed))
	}

	// Cycle 3: condition resolves (no longer undercut). Active alerts
	// should drop it, and its throttle state should clear.
	fake.MarketOrdersResp = nil
	cycle3 := cycle2.Add(1 * time.Hour)
	results, err = Run(ctx, fake, store, orders, cycle3)
	if err != nil {
		t.Fatalf("Run (cycle 3): %v", err)
	}
	if has(results[0].Alerts, Undercut) {
		t.Fatalf("cycle 3: Undercut still active after condition resolved, got %+v", results[0].Alerts)
	}
	if _, exists, err := store.GetOrderAlertState(ctx, order.OrderID, string(Undercut)); err != nil {
		t.Fatalf("GetOrderAlertState: %v", err)
	} else if exists {
		t.Error("alert state still exists after the condition resolved")
	}

	// Cycle 4: condition recurs. Since it fully resolved in between, this
	// is a NEW detection -- should fire again immediately, even though
	// cycle 4 is well within 4h of cycle 1's firing.
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.40},
	}
	cycle4 := cycle3.Add(1 * time.Minute)
	results, err = Run(ctx, fake, store, orders, cycle4)
	if err != nil {
		t.Fatalf("Run (cycle 4): %v", err)
	}
	if !has(results[0].Alerts, Undercut) {
		t.Fatalf("cycle 4: Undercut not active, got %+v", results[0].Alerts)
	}
	feed, err = store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 2 {
		t.Fatalf("feed entries after cycle 4 = %d, want 2 (resolved-then-recurred fires again)", len(feed))
	}
}

// TestRunFiresAgainAfterThrottleWindowElapses proves the "4 hours"
// half of "4 hours or until the condition resolves, whichever is first":
// a still-true condition fires again once 4h have passed since its last
// firing, without ever having resolved in between.
func TestRunFiresAgainAfterThrottleWindowElapses(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()

	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)
	order.OrderID = 7
	orders := []esi.Order{order}

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.40},
	}

	cycle1 := time.Now()
	if _, err := Run(ctx, fake, store, orders, cycle1); err != nil {
		t.Fatalf("Run (cycle 1): %v", err)
	}

	// Just under 4h: still suppressed.
	cycle2 := cycle1.Add(3*time.Hour + 59*time.Minute)
	if _, err := Run(ctx, fake, store, orders, cycle2); err != nil {
		t.Fatalf("Run (cycle 2): %v", err)
	}
	feed, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 1 {
		t.Fatalf("feed entries just under 4h = %d, want 1", len(feed))
	}

	// At/past 4h since the last firing: fires again.
	cycle3 := cycle1.Add(4 * time.Hour)
	if _, err := Run(ctx, fake, store, orders, cycle3); err != nil {
		t.Fatalf("Run (cycle 3): %v", err)
	}
	feed, err = store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 2 {
		t.Fatalf("feed entries at 4h = %d, want 2 (throttle window elapsed)", len(feed))
	}
}

// TestRunThrottlesAlertTypesIndependently proves Alerts are throttled
// per Order+Alert-type, not per Order: an Order that's both Undercut and
// Expiring gets independent throttle state for each.
func TestRunThrottlesAlertTypesIndependently(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()

	now := time.Now()
	order := sellOrder(100, now.Add(-89*24*time.Hour-20*time.Hour), 90) // expiring soon
	order.OrderID = 55
	orders := []esi.Order{order}

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 90}, // undercut + price-moved
	}

	if _, err := Run(ctx, fake, store, orders, now); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, at := range []AlertType{Undercut, PriceMoved, Expiring} {
		if _, exists, err := store.GetOrderAlertState(ctx, order.OrderID, string(at)); err != nil {
			t.Fatalf("GetOrderAlertState(%s): %v", at, err)
		} else if !exists {
			t.Errorf("alert state for %s does not exist, want it to", at)
		}
	}

	feed, err := store.RecentAlertFeed(ctx, 10)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 3 {
		t.Fatalf("feed entries = %d, want 3 (one per alert type)", len(feed))
	}
}

// TestConcurrentRunDoesNotDoubleFire guards against a race where two
// concurrent Run calls (e.g. two dashboard loads in flight at once) both
// observe "not yet fired" for the same Order+Alert-type and both record a
// firing. throttleMu serializes the read-decide-write sequence in
// applyThrottle; this launches many concurrent Run calls for a single
// new detection and asserts exactly one Alert Feed entry results.
func TestConcurrentRunDoesNotDoubleFire(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()

	order := sellOrder(5.50, time.Now().AddDate(0, 0, -1), 90)
	order.OrderID = 1
	orders := []esi.Order{order}

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 2, TypeID: order.TypeID, LocationID: order.LocationID, IsBuyOrder: false, Price: 5.40},
	}

	const concurrency = 20
	now := time.Now()

	var wg sync.WaitGroup
	errs := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Run(ctx, fake, store, orders, now); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("Run: %v", err)
	}

	feed, err := store.RecentAlertFeed(ctx, 100)
	if err != nil {
		t.Fatalf("RecentAlertFeed: %v", err)
	}
	if len(feed) != 1 {
		t.Fatalf("feed entries after %d concurrent Run calls = %d, want 1 (a new detection must fire exactly once)", concurrency, len(feed))
	}
}
