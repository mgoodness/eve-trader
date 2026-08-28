package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/internal/tracker"
)

func TestNotifySendsExpectedPayloadShape(t *testing.T) {
	var gotMethod, gotContentType string
	var gotPayload embedPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(srv.URL, "https://eve-trader.example/")
	err := n.Notify(context.Background(), AlertNotification{
		AlertType:         tracker.Undercut,
		Item:              "Tritanium",
		Hub:               "Jita",
		Detail:            "beaten: competing price 5.40 vs your 5.50",
		OrderPrice:        5.50,
		CompetingPrice:    5.40,
		HasCompetingPrice: true,
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	if len(gotPayload.Embeds) != 1 {
		t.Fatalf("len(embeds) = %d, want 1", len(gotPayload.Embeds))
	}
	e := gotPayload.Embeds[0]

	if e.Title != "Undercut" {
		t.Errorf("Title = %q, want Undercut", e.Title)
	}
	if e.Description != "beaten: competing price 5.40 vs your 5.50" {
		t.Errorf("Description = %q, want the Detail text", e.Description)
	}
	if e.Color != 0xf85149 {
		t.Errorf("Color = %#x, want %#x (red)", e.Color, 0xf85149)
	}
	if e.URL != "https://eve-trader.example/" {
		t.Errorf("URL = %q, want the dashboard URL", e.URL)
	}

	fieldValue := func(name string) (string, bool) {
		for _, f := range e.Fields {
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
}

func TestNotifyOmitsPriceFieldsWhenNotApplicable(t *testing.T) {
	var gotPayload embedPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(srv.URL, "https://eve-trader.example/")
	err := n.Notify(context.Background(), AlertNotification{
		AlertType: tracker.Expiring,
		Item:      "Tritanium",
		Hub:       "Jita",
		Detail:    "expires in 18h0m0s",
		// HasCompetingPrice left false: Expiring has no price to compare.
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	e := gotPayload.Embeds[0]
	if e.Title != "Expiring" {
		t.Errorf("Title = %q, want Expiring", e.Title)
	}
	if e.Color != 0xd29922 {
		t.Errorf("Color = %#x, want %#x (yellow)", e.Color, 0xd29922)
	}
	for _, f := range e.Fields {
		if f.Name == "Your Price" || f.Name == "Competing Price" {
			t.Errorf("unexpected price field %q for an Expiring alert", f.Name)
		}
	}
}

func TestNotifyColorsByAlertType(t *testing.T) {
	cases := []struct {
		alertType tracker.AlertType
		wantColor int
	}{
		{tracker.Undercut, 0xf85149},
		{tracker.Expiring, 0xd29922},
		{tracker.PriceMoved, 0x58a6ff},
	}

	for _, c := range cases {
		t.Run(string(c.alertType), func(t *testing.T) {
			var gotPayload embedPayload
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&gotPayload)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			n := New(srv.URL, "https://eve-trader.example/")
			if err := n.Notify(context.Background(), AlertNotification{AlertType: c.alertType, Detail: "x"}); err != nil {
				t.Fatalf("Notify: %v", err)
			}
			if gotPayload.Embeds[0].Color != c.wantColor {
				t.Errorf("Color = %#x, want %#x", gotPayload.Embeds[0].Color, c.wantColor)
			}
		})
	}
}

func TestNotifyNoOpWithoutWebhookURL(t *testing.T) {
	n := New("", "https://eve-trader.example/")

	if err := n.Notify(context.Background(), AlertNotification{AlertType: tracker.Undercut}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
}

func TestNotifyErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"invalid webhook"}`))
	}))
	defer srv.Close()

	n := New(srv.URL, "https://eve-trader.example/")
	if err := n.Notify(context.Background(), AlertNotification{AlertType: tracker.Undercut, Detail: "x"}); err == nil {
		t.Fatal("Notify: want error on non-2xx response, got nil")
	}
}
