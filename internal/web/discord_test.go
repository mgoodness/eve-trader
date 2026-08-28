package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/notify"
)

// discordEmbedPayload mirrors just enough of Discord's webhook payload
// shape to assert against in tests.
type discordEmbedPayload struct {
	Embeds []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Color       int    `json:"color"`
		URL         string `json:"url"`
		Fields      []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
	} `json:"embeds"`
}

// TestDashboardNotifiesDiscordOnceThenSuppressesDuringThrottleWindow is
// the AC-mandated test: a fake Discord webhook receives the expected
// payload shape when an Alert fires, and does not receive a duplicate
// during the throttle window.
func TestDashboardNotifiesDiscordOnceThenSuppressesDuringThrottleWindow(t *testing.T) {
	var postCount int32
	var lastPayload discordEmbedPayload

	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postCount, 1)
		if err := json.NewDecoder(r.Body).Decode(&lastPayload); err != nil {
			t.Fatalf("decode discord payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	store := openDashboardStore(t)
	fake := esi.NewFakeClient()
	// fake.Orders[0]: TypeID 34 (Tritanium), LocationID 60003760 (Jita),
	// sell @ 5.5 -- undercut by the competing 5.40 below.
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 999, TypeID: 34, LocationID: 60003760, IsBuyOrder: false, Price: 5.40},
	}
	notifier := notify.New(discord.URL, "https://eve-trader.example/")

	// Cycle 1: new detection -- Discord should receive exactly one embed.
	cycle1 := time.Now()
	if _, err := buildDashboardView(context.Background(), fake, store, notifier, 95465499, cycle1); err != nil {
		t.Fatalf("buildDashboardView (cycle 1): %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Fatalf("Discord POSTs after cycle 1 = %d, want 1", got)
	}

	if len(lastPayload.Embeds) != 1 {
		t.Fatalf("len(embeds) = %d, want 1", len(lastPayload.Embeds))
	}
	embed := lastPayload.Embeds[0]
	if embed.Title != "Undercut" {
		t.Errorf("embed title = %q, want Undercut", embed.Title)
	}
	if embed.URL != "https://eve-trader.example/" {
		t.Errorf("embed url = %q, want the dashboard URL", embed.URL)
	}
	fieldValue := func(name string) (string, bool) {
		for _, f := range embed.Fields {
			if f.Name == name {
				return f.Value, true
			}
		}
		return "", false
	}
	if v, ok := fieldValue("Item"); !ok || v != "Tritanium" {
		t.Errorf("Item field = %q (found=%v), want Tritanium", v, ok)
	}
	if v, ok := fieldValue("Hub"); !ok || v != "Jita" {
		t.Errorf("Hub field = %q (found=%v), want Jita", v, ok)
	}
	if v, ok := fieldValue("Your Price"); !ok || v != "5.50" {
		t.Errorf("Your Price field = %q (found=%v), want 5.50", v, ok)
	}
	if v, ok := fieldValue("Competing Price"); !ok || v != "5.40" {
		t.Errorf("Competing Price field = %q (found=%v), want 5.40", v, ok)
	}

	// Cycle 2: condition still true, well within the 4h throttle window --
	// Discord must NOT receive a duplicate.
	cycle2 := cycle1.Add(1 * time.Hour)
	if _, err := buildDashboardView(context.Background(), fake, store, notifier, 95465499, cycle2); err != nil {
		t.Fatalf("buildDashboardView (cycle 2): %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 1 {
		t.Errorf("Discord POSTs after cycle 2 (still throttled) = %d, want 1 (no duplicate)", got)
	}
}

func TestDashboardSkipsDiscordWhenWebhookNotConfigured(t *testing.T) {
	store := openDashboardStore(t)
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 999, TypeID: 34, LocationID: 60003760, IsBuyOrder: false, Price: 5.40},
	}

	// noopNotifier has an empty WebhookURL: the dashboard must still build
	// successfully (in-app Alerts keep working) without attempting delivery.
	view, err := buildDashboardView(context.Background(), fake, store, noopNotifier(), 95465499, time.Now())
	if err != nil {
		t.Fatalf("buildDashboardView: %v", err)
	}
	if len(view.Orders[0].Alerts) != 1 {
		t.Errorf("view.Orders[0].Alerts = %+v, want the Undercut badge to still render", view.Orders[0].Alerts)
	}
}
