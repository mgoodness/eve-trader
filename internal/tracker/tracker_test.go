package tracker

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
)

func sellOrder(price float64, issued time.Time, durationDays int32) esi.Order {
	return esi.Order{
		OrderID:      1,
		TypeID:       34,
		RegionID:     10000002,
		LocationID:   60003760,
		IsBuyOrder:   false,
		Price:        price,
		VolumeRemain: 100,
		Duration:     durationDays,
		Issued:       issued,
	}
}

func buyOrder(price float64, issued time.Time, durationDays int32) esi.Order {
	o := sellOrder(price, issued, durationDays)
	o.IsBuyOrder = true
	return o
}

func TestEvaluateUndercut(t *testing.T) {
	now := time.Now()
	placed := now.AddDate(0, 0, -1)

	cases := []struct {
		name         string
		order        esi.Order
		snap         MarketSnapshot
		wantUndercut bool
	}{
		{
			name:         "sell order beaten by a cheaper competing sell order",
			order:        sellOrder(5.50, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 5.49},
			wantUndercut: true,
		},
		{
			name:         "sell order beaten by the smallest possible increment",
			order:        sellOrder(5.50, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 5.49},
			wantUndercut: true,
		},
		{
			name:         "sell order not beaten -- competing price is higher",
			order:        sellOrder(5.50, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 5.60},
			wantUndercut: false,
		},
		{
			name:         "sell order matched exactly is not undercut",
			order:        sellOrder(5.50, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 5.50},
			wantUndercut: false,
		},
		{
			name:         "buy order beaten by a higher competing buy order",
			order:        buyOrder(62.0, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 62.5},
			wantUndercut: true,
		},
		{
			name:         "buy order not beaten -- competing price is lower",
			order:        buyOrder(62.0, placed, 90),
			snap:         MarketSnapshot{HasCompetition: true, BestCompetingPrice: 61.0},
			wantUndercut: false,
		},
		{
			name:         "no competition at all",
			order:        sellOrder(5.50, placed, 90),
			snap:         MarketSnapshot{HasCompetition: false},
			wantUndercut: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerts := Evaluate(c.order, c.snap, now)
			if has(alerts, Undercut) != c.wantUndercut {
				t.Errorf("Undercut present = %v, want %v (alerts: %+v)", has(alerts, Undercut), c.wantUndercut, alerts)
			}
		})
	}
}

func TestEvaluatePriceMoved(t *testing.T) {
	now := time.Now()
	placed := now.AddDate(0, 0, -1)

	cases := []struct {
		name           string
		order          esi.Order
		snap           MarketSnapshot
		wantPriceMoved bool
	}{
		{
			name:           "drifted more than 5% upward",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: true, BestCompetingPrice: 106},
			wantPriceMoved: true,
		},
		{
			name:           "drifted more than 5% downward",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: true, BestCompetingPrice: 94},
			wantPriceMoved: true,
		},
		{
			name:           "drifted exactly 5% does not fire (threshold is exclusive)",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: true, BestCompetingPrice: 105},
			wantPriceMoved: false,
		},
		{
			name:           "small drift under 5% does not fire",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: true, BestCompetingPrice: 102},
			wantPriceMoved: false,
		},
		{
			name:           "no competition at all",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: false},
			wantPriceMoved: false,
		},
		{
			name:           "fires without being undercut -- competing price rose well above (favorable direction)",
			order:          sellOrder(100, placed, 90),
			snap:           MarketSnapshot{HasCompetition: true, BestCompetingPrice: 200},
			wantPriceMoved: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerts := Evaluate(c.order, c.snap, now)
			if has(alerts, PriceMoved) != c.wantPriceMoved {
				t.Errorf("PriceMoved present = %v, want %v (alerts: %+v)", has(alerts, PriceMoved), c.wantPriceMoved, alerts)
			}
		})
	}
}

// TestPriceMovedDistinctFromUndercut proves the two conditions are
// evaluated independently: a competing price that's better for the
// trader (doesn't undercut) but has still drifted >5% from the order's
// placement price fires Price-Moved without Undercut.
func TestPriceMovedDistinctFromUndercut(t *testing.T) {
	now := time.Now()
	placed := now.AddDate(0, 0, -1)

	order := sellOrder(100, placed, 90)
	snap := MarketSnapshot{HasCompetition: true, BestCompetingPrice: 200} // higher, i.e. not undercutting a seller

	alerts := Evaluate(order, snap, now)
	if has(alerts, Undercut) {
		t.Error("Undercut should not fire when the competing price is worse for a buyer, not better")
	}
	if !has(alerts, PriceMoved) {
		t.Error("PriceMoved should fire on >5% drift even without an Undercut")
	}
}

// TestUndercutDetailUsesThousandsSeparators proves Undercut's Detail
// text (shown in the in-app Alert Feed and sent to Discord) groups
// large ISK prices with commas, not raw digit runs -- e.g. a PLEX-scale
// price should read "3,395,000.00", not "3395000.00".
func TestUndercutDetailUsesThousandsSeparators(t *testing.T) {
	now := time.Now()
	order := sellOrder(3410000, now.AddDate(0, 0, -1), 90)
	snap := MarketSnapshot{HasCompetition: true, BestCompetingPrice: 3395000}

	a, ok := evaluateUndercut(order, snap)
	if !ok {
		t.Fatal("evaluateUndercut: want fired, got not fired")
	}
	if want := "beaten: competing price 3,395,000.00 vs your 3,410,000.00"; a.Detail != want {
		t.Errorf("Detail = %q, want %q", a.Detail, want)
	}
}

// TestPriceMovedDetailUsesThousandsSeparators is TestUndercutDetail...'s
// counterpart for Price-Moved's Detail text.
func TestPriceMovedDetailUsesThousandsSeparators(t *testing.T) {
	now := time.Now()
	order := sellOrder(1000000, now.AddDate(0, 0, -1), 90)
	snap := MarketSnapshot{HasCompetition: true, BestCompetingPrice: 1200000}

	a, ok := evaluatePriceMoved(order, snap)
	if !ok {
		t.Fatal("evaluatePriceMoved: want fired, got not fired")
	}
	if want := "market drifted 20.0% since placement (1,000,000.00 -> 1,200,000.00)"; a.Detail != want {
		t.Errorf("Detail = %q, want %q", a.Detail, want)
	}
}

func TestEvaluateExpiring(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name         string
		issued       time.Time
		duration     int32
		wantExpiring bool
	}{
		{
			name:         "well within the 24h window",
			issued:       now.Add(-89*24*time.Hour - 20*time.Hour),
			duration:     90,
			wantExpiring: true,
		},
		{
			name:         "far from expiring",
			issued:       now,
			duration:     90,
			wantExpiring: false,
		},
		{
			name:         "already expired is not reported as expiring",
			issued:       now.Add(-91 * 24 * time.Hour),
			duration:     90,
			wantExpiring: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			order := sellOrder(5.5, c.issued, c.duration)
			alerts := Evaluate(order, MarketSnapshot{}, now)
			if has(alerts, Expiring) != c.wantExpiring {
				t.Errorf("Expiring present = %v, want %v (alerts: %+v)", has(alerts, Expiring), c.wantExpiring, alerts)
			}
		})
	}
}

func TestEvaluateMultipleAlertsSimultaneously(t *testing.T) {
	now := time.Now()
	// Issued so it's both expiring soon (within 24h) and placed long enough
	// ago that a >5%/undercutting drift is meaningful.
	issued := now.Add(-89*24*time.Hour - 20*time.Hour)

	order := sellOrder(100, issued, 90)
	snap := MarketSnapshot{HasCompetition: true, BestCompetingPrice: 90} // undercut + >5% drift

	alerts := Evaluate(order, snap, now)
	if !has(alerts, Undercut) || !has(alerts, PriceMoved) || !has(alerts, Expiring) {
		t.Errorf("expected all three alert types, got %+v", alerts)
	}
}

func has(alerts []EvaluatedAlert, t AlertType) bool {
	for _, a := range alerts {
		if a.Type == t {
			return true
		}
	}
	return false
}
