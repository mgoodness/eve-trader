package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/scanner"
	"github.com/mgoodness/eve-trader/internal/storage"
	"github.com/mgoodness/eve-trader/internal/tracker"
)

// noopNotifier is a Notifier with Discord delivery disabled, for tests
// that aren't exercising notification behavior specifically.
func noopNotifier() *notify.Notifier {
	return notify.New("", "https://eve-trader.example/")
}

func TestFormatTimeRemaining(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"already expired", -time.Hour, "expired"},
		{"zero", 0, "expired"},
		{"minutes only", 45 * time.Minute, "45m"},
		{"hours only", 18 * time.Hour, "18h"},
		{"days and hours", 6*24*time.Hour + 4*time.Hour, "6d 4h"},
		{"exactly one day", 24 * time.Hour, "1d 0h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatTimeRemaining(c.d); got != c.want {
				t.Errorf("formatTimeRemaining(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

func TestHubName(t *testing.T) {
	cases := []struct {
		locationID int64
		want       string
	}{
		{60003760, "Jita"},
		{60004588, "Rens"},
		{60012345, "Station 60012345"},
	}
	for _, c := range cases {
		if got := hubName(c.locationID); got != c.want {
			t.Errorf("hubName(%d) = %q, want %q", c.locationID, got, c.want)
		}
	}
}

func TestSideName(t *testing.T) {
	if got := sideName(true); got != "Buy" {
		t.Errorf("sideName(true) = %q, want Buy", got)
	}
	if got := sideName(false); got != "Sell" {
		t.Errorf("sideName(false) = %q, want Sell", got)
	}
}

func TestBuildOrderRows(t *testing.T) {
	now := time.Now()
	orders := []esi.Order{
		{
			OrderID:      1,
			TypeID:       34, // Tritanium
			LocationID:   60003760,
			IsBuyOrder:   false,
			Price:        5.5,
			VolumeRemain: 8000,
			Duration:     90,
			Issued:       now.Add(-time.Hour), // 89 days remain
		},
		{
			OrderID:      2,
			TypeID:       99999999, // unresolved
			LocationID:   60004588,
			IsBuyOrder:   true,
			Price:        62,
			VolumeRemain: 200000,
			Duration:     30,
			Issued:       now.Add(-time.Hour),
		},
	}
	names := map[int32]string{34: "Tritanium"}
	results := []tracker.OrderEvaluation{
		{Order: orders[0], Alerts: []tracker.EvaluatedAlert{{Type: tracker.Undercut, Detail: "beaten"}}},
		{Order: orders[1], Alerts: nil},
	}

	rows := buildOrderRows(orders, results, names, now)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	if rows[0].Item != "Tritanium" {
		t.Errorf("rows[0].Item = %q, want Tritanium", rows[0].Item)
	}
	if rows[0].Hub != "Jita" {
		t.Errorf("rows[0].Hub = %q, want Jita", rows[0].Hub)
	}
	if rows[0].Side != "Sell" {
		t.Errorf("rows[0].Side = %q, want Sell", rows[0].Side)
	}
	if rows[0].Price != "5.50" {
		t.Errorf("rows[0].Price = %q, want 5.50", rows[0].Price)
	}
	if rows[0].VolumeRemain != 8000 {
		t.Errorf("rows[0].VolumeRemain = %d, want 8000", rows[0].VolumeRemain)
	}
	if rows[0].TimeRemaining == "expired" || rows[0].TimeRemaining == "" {
		t.Errorf("rows[0].TimeRemaining = %q, want a non-expired duration", rows[0].TimeRemaining)
	}
	if len(rows[0].Alerts) != 1 || rows[0].Alerts[0].Label != "Undercut" || rows[0].Alerts[0].Class != "pill-undercut" {
		t.Errorf("rows[0].Alerts = %+v, want a single Undercut badge", rows[0].Alerts)
	}

	// Unresolved type ID falls back to a placeholder rather than an empty
	// name or an error.
	if rows[1].Item != "Item #99999999" {
		t.Errorf("rows[1].Item = %q, want Item #99999999", rows[1].Item)
	}
	if rows[1].Hub != "Rens" {
		t.Errorf("rows[1].Hub = %q, want Rens", rows[1].Hub)
	}
	if rows[1].Side != "Buy" {
		t.Errorf("rows[1].Side = %q, want Buy", rows[1].Side)
	}
	if len(rows[1].Alerts) != 0 {
		t.Errorf("rows[1].Alerts = %+v, want none", rows[1].Alerts)
	}
}

func TestAlertTypeLabelAndClass(t *testing.T) {
	cases := []struct {
		alertType tracker.AlertType
		label     string
		class     string
	}{
		{tracker.Undercut, "Undercut", "pill-undercut"},
		{tracker.Expiring, "Expiring", "pill-expiring"},
		{tracker.PriceMoved, "Price-Moved", "pill-pricemoved"},
	}
	for _, c := range cases {
		if got := alertTypeLabel(c.alertType); got != c.label {
			t.Errorf("alertTypeLabel(%s) = %q, want %q", c.alertType, got, c.label)
		}
		if got := alertTypeClass(c.alertType); got != c.class {
			t.Errorf("alertTypeClass(%s) = %q, want %q", c.alertType, got, c.class)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"minutes", now.Add(-2 * time.Minute), "2m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-5 * 24 * time.Hour), "5d ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relativeTime(now, c.t); got != c.want {
				t.Errorf("relativeTime(...) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBuildAlertFeedRows(t *testing.T) {
	now := time.Now()
	entries := []storage.AlertFeedEntry{
		{OrderID: 1, AlertType: "undercut", TypeID: 34, LocationID: 60003760, Detail: "beaten by 0.10", CreatedAt: now.Add(-2 * time.Minute)},
		{OrderID: 2, AlertType: "price_moved", TypeID: 99999999, LocationID: 60004588, Detail: "drifted 10%", CreatedAt: now.Add(-3 * time.Hour)},
	}
	names := map[int32]string{34: "Tritanium"}

	rows := buildAlertFeedRows(entries, names, now)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	if rows[0].When != "2m ago" || rows[0].Label != "Undercut" || rows[0].Class != "pill-undercut" || rows[0].Item != "Tritanium" || rows[0].Hub != "Jita" || rows[0].Detail != "beaten by 0.10" {
		t.Errorf("rows[0] = %+v, unexpected fields", rows[0])
	}
	if rows[1].When != "3h ago" || rows[1].Label != "Price-Moved" || rows[1].Item != "Item #99999999" || rows[1].Hub != "Rens" {
		t.Errorf("rows[1] = %+v, unexpected fields", rows[1])
	}
}

func openDashboardStore(t *testing.T) *storage.Store {
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

// TestBuildDashboardViewIncludesAlertsFromTracker is an integration test
// proving the dashboard wiring end-to-end: a fake ESIClient's canned
// market data undercuts the canned order, and that shows up both as a
// badge on the order row and as a new Alert Feed entry.
func TestBuildDashboardViewIncludesAlertsFromTracker(t *testing.T) {
	store := openDashboardStore(t)
	fake := esi.NewFakeClient()
	// fake.Orders[0]: TypeID 34, LocationID 60003760, sell @ 5.5.
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 999, TypeID: 34, LocationID: 60003760, IsBuyOrder: false, Price: 5.40},
	}

	view, err := buildDashboardView(context.Background(), fake, store, noopNotifier(), 95465499, hub.Jita, scanner.Filter{}, time.Now())
	if err != nil {
		t.Fatalf("buildDashboardView: %v", err)
	}

	if len(view.Orders) != 1 {
		t.Fatalf("len(view.Orders) = %d, want 1", len(view.Orders))
	}
	if len(view.Orders[0].Alerts) != 1 || view.Orders[0].Alerts[0].Label != "Undercut" {
		t.Errorf("view.Orders[0].Alerts = %+v, want a single Undercut badge", view.Orders[0].Alerts)
	}

	if len(view.AlertFeed) != 1 {
		t.Fatalf("len(view.AlertFeed) = %d, want 1", len(view.AlertFeed))
	}
	if view.AlertFeed[0].Label != "Undercut" || view.AlertFeed[0].Item != "Tritanium" || view.AlertFeed[0].Hub != "Jita" {
		t.Errorf("view.AlertFeed[0] = %+v, unexpected fields", view.AlertFeed[0])
	}
}

func TestBuildDashboardViewPropagatesESIError(t *testing.T) {
	store := openDashboardStore(t)
	fake := esi.NewFakeClient()
	fake.OrdersErr = context.DeadlineExceeded

	if _, err := buildDashboardView(context.Background(), fake, store, noopNotifier(), 1, hub.Jita, scanner.Filter{}, time.Now()); err == nil {
		t.Fatal("buildDashboardView: want error when CharacterOrders fails, got nil")
	}
}
