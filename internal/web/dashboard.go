package web

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/scanner"
	"github.com/mgoodness/eve-trader/internal/storage"
	"github.com/mgoodness/eve-trader/internal/tracker"
)

// alertFeedLimit bounds how many Alert Feed entries the dashboard shows.
const alertFeedLimit = 20

// opportunityLimit is the top end of the AC's "top 20-50 ranked results".
const opportunityLimit = 50

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

// opportunityRow is one row of the dashboard's Opportunity Scanner panel.
type opportunityRow struct {
	Item     string
	BestBuy  string
	BestSell string
	Margin   string
	Volume   string
}

// hubTab is one Hub tab in the Opportunity Scanner panel's header.
type hubTab struct {
	Name   string
	URL    string
	Active bool
}

// dashboardView is the data passed to the dashboard template.
type dashboardView struct {
	Authenticated bool
	LoginURL      string
	Orders        []orderRow
	AlertFeed     []alertFeedRow

	HubTabs         []hubTab
	SelectedHubName string
	MinVolume       float64
	MinMargin       float64
	Opportunities   []opportunityRow
}

// buildDashboardView fetches the character's open orders, runs the
// OrderTracker's evaluation cycle against them (updating Alert throttle
// state and the Alert Feed as a side effect), sends a Discord embed for
// each newly-fired Alert, runs the Opportunity Scanner for selectedHub,
// and assembles the dashboard's view model.
func buildDashboardView(ctx context.Context, client esi.Client, store *storage.Store, notifier *notify.Notifier, characterID int32, selectedHub hub.Hub, filter scanner.Filter, now time.Time) (dashboardView, error) {
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

	opportunities, err := scanner.Scan(ctx, client, store, selectedHub, characterID, now)
	if err != nil {
		return dashboardView{}, fmt.Errorf("scan opportunities: %w", err)
	}
	ranked := scanner.Rank(opportunities, filter, opportunityLimit)

	oppNames, err := client.ResolveNames(ctx, opportunityTypeIDs(ranked))
	if err != nil {
		return dashboardView{}, fmt.Errorf("resolve opportunity item names: %w", err)
	}

	return dashboardView{
		Authenticated:   true,
		Orders:          buildOrderRows(orders, results, names, now),
		AlertFeed:       buildAlertFeedRows(feedEntries, names, now),
		HubTabs:         buildHubTabs(selectedHub, filter),
		SelectedHubName: selectedHub.Name,
		MinVolume:       filter.MinVolume,
		MinMargin:       filter.MinMargin,
		Opportunities:   buildOpportunityRows(ranked, oppNames),
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

// opportunityTypeIDs collects the distinct item type IDs across ranked
// Opportunities, for a single ResolveNames call.
func opportunityTypeIDs(opportunities []scanner.Opportunity) []int32 {
	ids := make([]int32, len(opportunities))
	for i, o := range opportunities {
		ids[i] = o.TypeID
	}
	return ids
}

// buildOpportunityRows resolves each ranked Opportunity into a
// dashboard-ready row (Rank has already sorted/filtered/truncated).
func buildOpportunityRows(opportunities []scanner.Opportunity, names map[int32]string) []opportunityRow {
	rows := make([]opportunityRow, len(opportunities))
	for i, o := range opportunities {
		rows[i] = opportunityRow{
			Item:     itemName(names, o.TypeID),
			BestBuy:  fmt.Sprintf("%.2f", o.BestBuy),
			BestSell: fmt.Sprintf("%.2f", o.BestSell),
			Margin:   fmt.Sprintf("%.2f", o.Margin),
			Volume:   fmt.Sprintf("%.0f", o.AvgDailyVolume),
		}
	}
	return rows
}

// buildHubTabs builds the Opportunity panel's Hub tab links, preserving
// the current filter's query parameters when switching Hubs.
func buildHubTabs(selected hub.Hub, filter scanner.Filter) []hubTab {
	tabs := make([]hubTab, len(hub.All))
	for i, h := range hub.All {
		q := url.Values{"hub": {h.Name}}
		if filter.MinVolume > 0 {
			q.Set("minVolume", strconv.FormatFloat(filter.MinVolume, 'f', -1, 64))
		}
		if filter.MinMargin > 0 {
			q.Set("minMargin", strconv.FormatFloat(filter.MinMargin, 'f', -1, 64))
		}
		tabs[i] = hubTab{Name: h.Name, URL: "/?" + q.Encode(), Active: h.Name == selected.Name}
	}
	return tabs
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
	if h, ok := hub.ByStationID(locationID); ok {
		return h.Name
	}
	return fmt.Sprintf("Station %d", locationID)
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
