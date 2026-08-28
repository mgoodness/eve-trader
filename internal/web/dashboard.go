package web

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/storage"
	"github.com/mgoodness/eve-trader/internal/tracker"
)

// Station IDs for the two Hubs this tool trades at (see CONTEXT.md).
const (
	jitaStationID = 60003760
	rensStationID = 60004588
)

// alertFeedLimit bounds how many Alert Feed entries the dashboard shows.
const alertFeedLimit = 20

// orderRow is one row of the dashboard's Orders sidebar.
type orderRow struct {
	Item          string
	Hub           string
	Side          string
	Price         string
	VolumeRemain  int32
	TimeRemaining string
	Alerts        []alertBadge
}

// alertBadge is one color-coded badge on an Order row.
type alertBadge struct {
	Label string
	Class string
}

// alertFeedRow is one entry in the dashboard's chronological Alert Feed.
type alertFeedRow struct {
	When   string
	Label  string
	Class  string
	Item   string
	Hub    string
	Detail string
}

// dashboardView is the data passed to the dashboard template.
type dashboardView struct {
	Authenticated bool
	LoginURL      string
	Orders        []orderRow
	AlertFeed     []alertFeedRow
}

// buildDashboardView fetches the character's open orders, runs the
// OrderTracker's evaluation cycle against them (updating Alert throttle
// state and the Alert Feed as a side effect), sends a Discord embed for
// each newly-fired Alert, and assembles the dashboard's view model.
func buildDashboardView(ctx context.Context, client esi.Client, store *storage.Store, notifier *notify.Notifier, characterID int32, now time.Time) (dashboardView, error) {
	orders, err := client.CharacterOrders(ctx, characterID)
	if err != nil {
		return dashboardView{}, fmt.Errorf("fetch character orders: %w", err)
	}

	results, fired, err := tracker.Run(ctx, client, store, orders, now)
	if err != nil {
		return dashboardView{}, fmt.Errorf("evaluate alerts: %w", err)
	}

	feedEntries, err := store.RecentAlertFeed(ctx, alertFeedLimit)
	if err != nil {
		return dashboardView{}, fmt.Errorf("load alert feed: %w", err)
	}

	names, err := resolveNamesFor(ctx, client, orders, feedEntries)
	if err != nil {
		return dashboardView{}, fmt.Errorf("resolve item names: %w", err)
	}

	// A Discord hiccup shouldn't break the dashboard the trader is
	// actually looking at -- log and move on rather than failing the
	// request.
	for _, f := range fired {
		if err := notifier.Notify(ctx, alertNotification(f, names)); err != nil {
			log.Printf("discord notify: %v", err)
		}
	}

	return dashboardView{
		Authenticated: true,
		Orders:        buildOrderRows(orders, results, names, now),
		AlertFeed:     buildAlertFeedRows(feedEntries, names, now),
	}, nil
}

// alertNotification resolves a fired Alert's display data (item name,
// Hub) for Discord delivery.
func alertNotification(f tracker.FiredAlert, names map[int32]string) notify.AlertNotification {
	return notify.AlertNotification{
		AlertType:         f.Alert.Type,
		Item:              itemName(names, f.Order.TypeID),
		Hub:               hubName(f.Order.LocationID),
		Detail:            f.Alert.Detail,
		OrderPrice:        f.Alert.OrderPrice,
		CompetingPrice:    f.Alert.CompetingPrice,
		HasCompetingPrice: f.Alert.HasCompetingPrice,
	}
}

// resolveNamesFor resolves the item names needed by both the Orders
// sidebar and the Alert Feed in a single ESIClient call.
func resolveNamesFor(ctx context.Context, client esi.Client, orders []esi.Order, feed []storage.AlertFeedEntry) (map[int32]string, error) {
	seen := make(map[int32]bool, len(orders)+len(feed))
	var ids []int32
	add := func(id int32) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, o := range orders {
		add(o.TypeID)
	}
	for _, e := range feed {
		add(e.TypeID)
	}
	return client.ResolveNames(ctx, ids)
}

// buildOrderRows resolves each open order into a dashboard-ready row,
// including its currently-active Alert badges (from results, keyed by
// OrderID).
func buildOrderRows(orders []esi.Order, results []tracker.OrderEvaluation, names map[int32]string, now time.Time) []orderRow {
	alertsByOrder := make(map[int64][]tracker.EvaluatedAlert, len(results))
	for _, r := range results {
		alertsByOrder[r.Order.OrderID] = r.Alerts
	}

	rows := make([]orderRow, len(orders))
	for i, o := range orders {
		expiresAt := o.Issued.AddDate(0, 0, int(o.Duration))
		rows[i] = orderRow{
			Item:          itemName(names, o.TypeID),
			Hub:           hubName(o.LocationID),
			Side:          sideName(o.IsBuyOrder),
			Price:         fmt.Sprintf("%.2f", o.Price),
			VolumeRemain:  o.VolumeRemain,
			TimeRemaining: formatTimeRemaining(expiresAt.Sub(now)),
			Alerts:        alertBadges(alertsByOrder[o.OrderID]),
		}
	}
	return rows
}

// buildAlertFeedRows resolves each Alert Feed entry into a dashboard-ready
// row, most-recent-first (the order RecentAlertFeed already returns them
// in).
func buildAlertFeedRows(entries []storage.AlertFeedEntry, names map[int32]string, now time.Time) []alertFeedRow {
	rows := make([]alertFeedRow, len(entries))
	for i, e := range entries {
		alertType := tracker.AlertType(e.AlertType)
		rows[i] = alertFeedRow{
			When:   relativeTime(now, e.CreatedAt),
			Label:  alertTypeLabel(alertType),
			Class:  alertTypeClass(alertType),
			Item:   itemName(names, e.TypeID),
			Hub:    hubName(e.LocationID),
			Detail: e.Detail,
		}
	}
	return rows
}

func alertBadges(alerts []tracker.EvaluatedAlert) []alertBadge {
	badges := make([]alertBadge, len(alerts))
	for i, a := range alerts {
		badges[i] = alertBadge{Label: alertTypeLabel(a.Type), Class: alertTypeClass(a.Type)}
	}
	return badges
}

// alertTypeDisplay renders an AlertType the way the chosen dashboard
// prototype does: red=Undercut, yellow=Expiring, blue=Price-Moved.
var alertTypeDisplay = map[tracker.AlertType]struct{ Label, Class string }{
	tracker.Undercut:   {Label: "Undercut", Class: "pill-undercut"},
	tracker.Expiring:   {Label: "Expiring", Class: "pill-expiring"},
	tracker.PriceMoved: {Label: "Price-Moved", Class: "pill-pricemoved"},
}

func alertTypeLabel(t tracker.AlertType) string {
	if d, ok := alertTypeDisplay[t]; ok {
		return d.Label
	}
	return string(t)
}

func alertTypeClass(t tracker.AlertType) string {
	if d, ok := alertTypeDisplay[t]; ok {
		return d.Class
	}
	return "pill"
}

func itemName(names map[int32]string, typeID int32) string {
	if name, ok := names[typeID]; ok {
		return name
	}
	return fmt.Sprintf("Item #%d", typeID)
}

func hubName(locationID int64) string {
	switch locationID {
	case jitaStationID:
		return "Jita"
	case rensStationID:
		return "Rens"
	default:
		return fmt.Sprintf("Station %d", locationID)
	}
}

func sideName(isBuyOrder bool) string {
	if isBuyOrder {
		return "Buy"
	}
	return "Sell"
}

// formatTimeRemaining renders a duration the way the chosen dashboard
// prototype does: "6d 4h", "18h", or "45m", falling back to "expired" for
// a non-positive duration.
func formatTimeRemaining(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}

	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	minutes := int(d/time.Minute) % 60
	return fmt.Sprintf("%dm", minutes)
}

// relativeTime renders how long ago t was, relative to now, the way the
// chosen dashboard prototype's Alert Feed does: "2m ago", "3h ago".
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
