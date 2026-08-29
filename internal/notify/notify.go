// Package notify implements the Notifier: rendering and sending a
// Discord embed for each newly-fired Alert.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mgoodness/eve-trader/internal/numfmt"
	"github.com/mgoodness/eve-trader/internal/tracker"
)

// alertDisplay is a Discord-embed rendering of an AlertType: Title and
// Color (24-bit RGB), matching the dashboard badge colors (see
// CONTEXT.md's Alert definition: red=Undercut, yellow=Expiring,
// blue=Price-Moved).
var alertDisplay = map[tracker.AlertType]struct {
	Title string
	Color int
}{
	tracker.Undercut:   {Title: "Undercut", Color: 0xf85149},
	tracker.Expiring:   {Title: "Expiring", Color: 0xd29922},
	tracker.PriceMoved: {Title: "Price-Moved", Color: 0x58a6ff},
}

// AlertNotification is the already-resolved data needed to render one
// Discord embed for a newly-fired Alert. OrderPrice/CompetingPrice are
// only rendered when HasCompetingPrice is true (Undercut, Price-Moved);
// Expiring has no price to compare, so Detail alone describes it.
type AlertNotification struct {
	AlertType         tracker.AlertType
	Item              string
	Hub               string
	Detail            string
	OrderPrice        float64
	CompetingPrice    float64
	HasCompetingPrice bool
}

// Notifier sends Discord webhook embeds for newly-fired Alerts.
type Notifier struct {
	HTTPClient *http.Client

	// WebhookURL is the Discord webhook to POST embeds to. Empty
	// disables delivery entirely (e.g. for local development without a
	// configured Discord webhook) -- Notify becomes a no-op.
	WebhookURL string

	// DashboardURL is the link-back target embedded in every Alert.
	DashboardURL string
}

// New builds a Notifier. Pass an empty webhookURL to disable Discord
// delivery; Notify will then simply do nothing.
func New(webhookURL, dashboardURL string) *Notifier {
	return &Notifier{
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		WebhookURL:   webhookURL,
		DashboardURL: dashboardURL,
	}
}

// embedPayload is Discord's webhook execute request body (the subset of
// fields this app uses).
type embedPayload struct {
	Embeds []embed `json:"embeds"`
}

type embed struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Color       int          `json:"color"`
	URL         string       `json:"url,omitempty"`
	Fields      []embedField `json:"fields"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// Notify sends a Discord embed for n. It's a no-op if no webhook URL is
// configured.
func (nf *Notifier) Notify(ctx context.Context, n AlertNotification) error {
	if nf.WebhookURL == "" {
		return nil
	}

	display := alertDisplay[n.AlertType]

	fields := []embedField{
		{Name: "Item", Value: n.Item, Inline: true},
		{Name: "Hub", Value: n.Hub, Inline: true},
	}
	if n.HasCompetingPrice {
		fields = append(fields,
			embedField{Name: "Your Price", Value: numfmt.FormatFloat(n.OrderPrice, 2), Inline: true},
			embedField{Name: "Competing Price", Value: numfmt.FormatFloat(n.CompetingPrice, 2), Inline: true},
		)
	}

	payload := embedPayload{
		Embeds: []embed{
			{
				Title:       display.Title,
				Description: n.Detail,
				Color:       display.Color,
				URL:         nf.DashboardURL,
				Fields:      fields,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nf.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := nf.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("discord webhook: unexpected status %s: %s", resp.Status, detail)
	}
	return nil
}
