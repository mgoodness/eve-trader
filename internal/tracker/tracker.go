// Package tracker implements the OrderTracker: evaluating the trader's
// Orders against live market state to detect Undercut, Expiring, and
// Price-Moved conditions, with per-Order throttling of repeat Alerts.
package tracker

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// AlertType is one of the three conditions OrderTracker detects.
type AlertType string

const (
	Undercut   AlertType = "undercut"
	PriceMoved AlertType = "price_moved"
	Expiring   AlertType = "expiring"
)

const (
	// priceMovedThreshold is the fraction of drift from an Order's
	// placement price that triggers Price-Moved (see CONTEXT.md).
	priceMovedThreshold = 0.05

	// expiringWindow is how far ahead of expiration Expiring fires.
	expiringWindow = 24 * time.Hour

	// throttleWindow is how long a repeat Alert is suppressed for the
	// same Order+Alert-type (see CONTEXT.md's Alert definition).
	throttleWindow = 4 * time.Hour

	// priceEpsilon absorbs float64 rounding noise around ESI's 0.01 ISK
	// minimum price increment, so two prices that are "equal" up to that
	// increment never spuriously register as an Undercut.
	priceEpsilon = 0.005

	// maxMarketPages bounds FetchSnapshot's pagination loop -- a sane
	// upper limit well above the largest observed regional order-book
	// page count (~411 pages for The Forge/Jita, per
	// docs/research/esi-market-endpoints.md), so a malformed response
	// can't spin forever.
	maxMarketPages = 500
)

// MarketSnapshot is the best competing price at an Order's Hub+item, on
// the Order's own side, excluding the Order itself.
//
// "Best competing price" is deliberately not the same as "the best price
// including the trader's own order": if the trader hasn't been beaten,
// the two would always be identical, and Price-Moved could then never
// fire without an Undercut also firing -- but the spec defines them as
// independent conditions. Excluding the trader's own order lets the
// market's general drift show up even when nobody has technically beaten
// them yet (see docs/adr/0005-price-moved-excludes-own-order.md).
type MarketSnapshot struct {
	BestCompetingPrice float64
	HasCompetition     bool
}

// EvaluatedAlert is one Alert condition found true for an Order, with a
// human-readable Detail for the Alert Feed.
//
// OrderPrice and CompetingPrice are only meaningful when HasCompetingPrice
// is true (Undercut and Price-Moved); Expiring has no price to compare,
// so a renderer should fall back to Detail alone for that type.
type EvaluatedAlert struct {
	Type              AlertType
	Detail            string
	OrderPrice        float64
	CompetingPrice    float64
	HasCompetingPrice bool
}

// Evaluate returns every Alert condition currently true for order, given
// the current best competing price at its Hub+item and side.
func Evaluate(order esi.Order, snap MarketSnapshot, now time.Time) []EvaluatedAlert {
	var alerts []EvaluatedAlert

	if a, ok := evaluateUndercut(order, snap); ok {
		alerts = append(alerts, a)
	}
	if a, ok := evaluatePriceMoved(order, snap); ok {
		alerts = append(alerts, a)
	}
	if a, ok := evaluateExpiring(order, now); ok {
		alerts = append(alerts, a)
	}
	return alerts
}

func evaluateUndercut(order esi.Order, snap MarketSnapshot) (EvaluatedAlert, bool) {
	if !snap.HasCompetition {
		return EvaluatedAlert{}, false
	}

	beaten := false
	if order.IsBuyOrder {
		beaten = snap.BestCompetingPrice > order.Price+priceEpsilon
	} else {
		beaten = snap.BestCompetingPrice < order.Price-priceEpsilon
	}
	if !beaten {
		return EvaluatedAlert{}, false
	}

	return EvaluatedAlert{
		Type:              Undercut,
		Detail:            fmt.Sprintf("beaten: competing price %.2f vs your %.2f", snap.BestCompetingPrice, order.Price),
		OrderPrice:        order.Price,
		CompetingPrice:    snap.BestCompetingPrice,
		HasCompetingPrice: true,
	}, true
}

func evaluatePriceMoved(order esi.Order, snap MarketSnapshot) (EvaluatedAlert, bool) {
	if !snap.HasCompetition || order.Price == 0 {
		return EvaluatedAlert{}, false
	}

	drift := (snap.BestCompetingPrice - order.Price) / order.Price
	if math.Abs(drift) <= priceMovedThreshold {
		return EvaluatedAlert{}, false
	}

	return EvaluatedAlert{
		Type:              PriceMoved,
		Detail:            fmt.Sprintf("market drifted %.1f%% since placement (%.2f -> %.2f)", drift*100, order.Price, snap.BestCompetingPrice),
		OrderPrice:        order.Price,
		CompetingPrice:    snap.BestCompetingPrice,
		HasCompetingPrice: true,
	}, true
}

func evaluateExpiring(order esi.Order, now time.Time) (EvaluatedAlert, bool) {
	expiresAt := order.Issued.AddDate(0, 0, int(order.Duration))
	remaining := expiresAt.Sub(now)
	if remaining <= 0 || remaining > expiringWindow {
		return EvaluatedAlert{}, false
	}

	return EvaluatedAlert{
		Type:   Expiring,
		Detail: fmt.Sprintf("expires in %s", remaining.Round(time.Minute)),
	}, true
}

// FetchSnapshot fetches the current best competing price for order's
// Hub+item+side, scoping the region's order book to order's item type
// server-side (ESI's type_id filter -- fetching a region unfiltered is
// far too slow to do synchronously per order) and filtering client-side
// to matching orders at the same location, same side, excluding order
// itself.
func FetchSnapshot(ctx context.Context, client esi.Client, order esi.Order) (MarketSnapshot, error) {
	orderType := esi.OrderTypeSell
	if order.IsBuyOrder {
		orderType = esi.OrderTypeBuy
	}

	var snap MarketSnapshot
	for page := int32(1); page <= maxMarketPages; page++ {
		entries, err := client.MarketOrders(ctx, order.RegionID, orderType, order.TypeID, page)
		if err != nil {
			return MarketSnapshot{}, fmt.Errorf("fetch market orders (page %d): %w", page, err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			if e.OrderID == order.OrderID || e.LocationID != order.LocationID || e.TypeID != order.TypeID {
				continue
			}
			if !snap.HasCompetition {
				snap = MarketSnapshot{HasCompetition: true, BestCompetingPrice: e.Price}
				continue
			}
			if order.IsBuyOrder && e.Price > snap.BestCompetingPrice {
				snap.BestCompetingPrice = e.Price
			}
			if !order.IsBuyOrder && e.Price < snap.BestCompetingPrice {
				snap.BestCompetingPrice = e.Price
			}
		}
	}
	return snap, nil
}

// OrderEvaluation is the outcome of evaluating one Order: its
// currently-active Alerts (for badge rendering -- live, not throttled).
type OrderEvaluation struct {
	Order  esi.Order
	Alerts []EvaluatedAlert
}

// allAlertTypes is every AlertType Run must check per Order, so a
// condition that stops being true gets its throttle state cleared even
// though Evaluate no longer reports it.
var allAlertTypes = []AlertType{Undercut, PriceMoved, Expiring}

// FiredAlert is an Alert that actually fired this cycle -- a new
// detection, or a repeat after the throttle window elapsed -- as opposed
// to one that's merely still active but suppressed. This is the set a
// Notifier should send to Discord: sending on every active Alert would
// re-notify on every suppressed evaluation cycle too.
type FiredAlert struct {
	Order esi.Order
	Alert EvaluatedAlert
}

// Run evaluates every Order's live conditions, applies the per-Order
// throttling rule (recording a new Alert Feed entry only on new
// detection or after the throttle window elapses), and returns each
// Order's currently-active Alerts for badge rendering, plus the Alerts
// that actually fired this cycle (for Discord delivery).
func Run(ctx context.Context, client esi.Client, store *storage.Store, orders []esi.Order, now time.Time) ([]OrderEvaluation, []FiredAlert, error) {
	results := make([]OrderEvaluation, len(orders))
	var fired []FiredAlert

	for i, order := range orders {
		snap, err := FetchSnapshot(ctx, client, order)
		if err != nil {
			return nil, nil, fmt.Errorf("order %d: %w", order.OrderID, err)
		}

		active := Evaluate(order, snap, now)
		results[i] = OrderEvaluation{Order: order, Alerts: active}

		activeByType := make(map[AlertType]EvaluatedAlert, len(active))
		for _, a := range active {
			activeByType[a.Type] = a
		}

		for _, t := range allAlertTypes {
			a, isActive := activeByType[t]
			didFire, err := applyThrottle(ctx, store, order, t, isActive, a.Detail, now)
			if err != nil {
				return nil, nil, fmt.Errorf("order %d, alert %s: %w", order.OrderID, t, err)
			}
			if didFire {
				fired = append(fired, FiredAlert{Order: order, Alert: a})
			}
		}
	}

	return results, fired, nil
}

// throttleMu serializes applyThrottle's read-then-write against
// order_alert_state and alert_feed. Run is called once per dashboard
// load, and net/http serves each request on its own goroutine, so two
// concurrent loads (a double-click, two open tabs) could otherwise both
// read "not yet fired" for the same Order+Alert-type and both write a
// firing -- this makes that read-decide-write sequence atomic instead.
var throttleMu sync.Mutex

// applyThrottle records a new Alert Feed entry -- and reports fired=true
// -- only when isActive is a new detection or the throttle window has
// elapsed since the last firing; clears state entirely once the
// condition resolves, so its next occurrence is treated as new rather
// than a suppressed repeat.
func applyThrottle(ctx context.Context, store *storage.Store, order esi.Order, alertType AlertType, isActive bool, detail string, now time.Time) (fired bool, err error) {
	throttleMu.Lock()
	defer throttleMu.Unlock()

	lastFiredAt, exists, err := store.GetOrderAlertState(ctx, order.OrderID, string(alertType))
	if err != nil {
		return false, fmt.Errorf("load alert state: %w", err)
	}

	if !isActive {
		if !exists {
			return false, nil
		}
		if err := store.ClearOrderAlertState(ctx, order.OrderID, string(alertType)); err != nil {
			return false, fmt.Errorf("clear alert state: %w", err)
		}
		return false, nil
	}

	if exists && now.Sub(lastFiredAt) < throttleWindow {
		return false, nil
	}

	if err := store.UpsertOrderAlertState(ctx, order.OrderID, string(alertType), now); err != nil {
		return false, fmt.Errorf("update alert state: %w", err)
	}
	if err := store.InsertAlertFeedEntry(ctx, storage.AlertFeedEntry{
		OrderID:    order.OrderID,
		AlertType:  string(alertType),
		TypeID:     order.TypeID,
		LocationID: order.LocationID,
		Detail:     detail,
		CreatedAt:  now,
	}); err != nil {
		return false, fmt.Errorf("insert alert feed entry: %w", err)
	}
	return true, nil
}
