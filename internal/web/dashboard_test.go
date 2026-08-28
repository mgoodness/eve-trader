package web

import (
	"context"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
)

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
	fake := esi.NewFakeClient()
	fake.Orders = []esi.Order{
		{
			OrderID:      1,
			TypeID:       34, // Tritanium, resolved by FakeClient.Names
			LocationID:   60003760,
			IsBuyOrder:   false,
			Price:        5.5,
			VolumeRemain: 8000,
			Duration:     90,
			Issued:       time.Now().Add(-time.Hour), // 89 days remain
		},
		{
			OrderID:      2,
			TypeID:       99999999, // unknown to FakeClient.Names
			LocationID:   60004588,
			IsBuyOrder:   true,
			Price:        62,
			VolumeRemain: 200000,
			Duration:     30,
			Issued:       time.Now().Add(-time.Hour),
		},
	}

	rows, err := buildOrderRows(context.Background(), fake, 95465499)
	if err != nil {
		t.Fatalf("buildOrderRows: %v", err)
	}
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
}

func TestBuildOrderRowsPropagatesESIError(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.OrdersErr = context.DeadlineExceeded

	if _, err := buildOrderRows(context.Background(), fake, 1); err == nil {
		t.Fatal("buildOrderRows: want error when CharacterOrders fails, got nil")
	}
}
