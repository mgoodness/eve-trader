package web

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/mgoodness/eve-trader/internal/auth"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/scanner"
	"github.com/mgoodness/eve-trader/internal/storage"
	"github.com/mgoodness/eve-trader/internal/tracker"
)

// BackgroundRefreshInterval is how often a background Refresh Cycle
// runs independent of any HTTP request -- the "otherwise every 4 hours"
// idle floor. While a dashboard request keeps arriving (e.g. the
// authenticated view's own auto-reload), the per-request refresh logic
// (order-book/snapshot caches on their own 5-minute TTLs) already keeps
// things fresh on its own, faster than this floor.
const BackgroundRefreshInterval = 4 * time.Hour

// StartBackgroundRefresh runs one Refresh Cycle immediately, then every
// BackgroundRefreshInterval thereafter until ctx is canceled -- so a
// freshly-deployed instance (this app deploys continuously, see
// ADR-0008) doesn't wait a full interval before its first idle-mode
// cycle. Each cycle evaluates the currently-authenticated character's
// Orders (firing any due Alerts, exactly as a dashboard request would)
// and scans every Hub. Runs in its own goroutine; callers don't need to
// wait on it.
func StartBackgroundRefresh(ctx context.Context, client esi.Client, store *storage.Store, notifier *notify.Notifier) {
	go func() {
		runBackgroundCycle(ctx, client, store, notifier, time.Now())

		ticker := time.NewTicker(BackgroundRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				runBackgroundCycle(ctx, client, store, notifier, now)
			}
		}
	}()
}

// runBackgroundCycle performs one Refresh Cycle: the same Order
// evaluation (firing due Alerts via notifier) and per-Hub Scan a
// dashboard request triggers, but independent of one. A cycle with no
// currently-authenticated character skips gracefully -- nobody's Orders
// to evaluate yet, not an error. Failures are logged and skip only the
// affected step, so one Hub's scan failing doesn't stop Order
// evaluation or the other Hub's scan.
func runBackgroundCycle(ctx context.Context, client esi.Client, store *storage.Store, notifier *notify.Notifier, now time.Time) {
	characterID, err := auth.CurrentCharacterID(ctx, store)
	if errors.Is(err, storage.ErrNoToken) {
		return
	}
	if err != nil {
		log.Printf("background refresh: resolve character: %v", err)
		return
	}

	orders, err := client.CharacterOrders(ctx, characterID)
	if err != nil {
		log.Printf("background refresh: fetch character orders: %v", err)
	} else if _, fired, err := tracker.Run(ctx, client, store, orders, now); err != nil {
		log.Printf("background refresh: evaluate alerts: %v", err)
	} else if len(fired) > 0 {
		notifyFired(ctx, client, notifier, fired)
	}

	for _, h := range hub.All {
		if _, err := scanner.Scan(ctx, client, store, h, characterID, now); err != nil {
			log.Printf("background refresh: scan %s: %v", h.Name, err)
		}
	}
}

// notifyFired resolves item names for fired's Orders and sends each a
// Discord notification, logging (rather than failing the whole cycle)
// on any error -- a Discord hiccup shouldn't block Order evaluation or
// scanning, same rationale as buildDashboardView's own Discord delivery.
func notifyFired(ctx context.Context, client esi.Client, notifier *notify.Notifier, fired []tracker.FiredAlert) {
	firedOrders := make([]esi.Order, len(fired))
	for i, f := range fired {
		firedOrders[i] = f.Order
	}
	names, err := resolveNamesFor(ctx, client, firedOrders, nil)
	if err != nil {
		log.Printf("background refresh: resolve item names: %v", err)
		return
	}
	for _, f := range fired {
		if err := notifier.Notify(ctx, alertNotification(f, names)); err != nil {
			log.Printf("background refresh: discord notify: %v", err)
		}
	}
}
