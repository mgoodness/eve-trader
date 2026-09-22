package fees_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/fees"
)

func TestBrokerFeeRate(t *testing.T) {
	tests := []struct {
		name                          string
		level                         int
		corpStanding, factionStanding float64
		want                          float64
	}{
		{"no standings", 0, 0, 0, 0.03},
		{"skill only, no standings", 4, 0, 0, 0.018},
		{"max skill, no standings", 5, 0, 0, 0.015},
		// −0.03%×10 − 0.02%×10 = −0.5%, so 3% − 0.5% = 2.5% at level 0.
		{"max positive standings", 0, 10, 10, 0.025},
		// Max skill and max standings reach EVE's 1% floor.
		{"max skill and standings", 5, 10, 10, 0.01},
		// Negative standings raise the rate above 3%.
		{"negative standings", 0, -10, -10, 0.035},
	}
	for _, tc := range tests {
		if got := fees.BrokerFeeRate(tc.level, tc.corpStanding, tc.factionStanding); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("%s: BrokerFeeRate(%d, %v, %v) = %v, want %v", tc.name, tc.level, tc.corpStanding, tc.factionStanding, got, tc.want)
		}
	}
}

func TestSalesTaxRate(t *testing.T) {
	tests := []struct {
		level int
		want  float64
	}{
		{0, 0.075},
		{3, 0.075 * (1 - 0.11*3)},
		{5, 0.075 * (1 - 0.11*5)},
	}
	for _, tc := range tests {
		if got := fees.SalesTaxRate(tc.level); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("SalesTaxRate(%d) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestPlacementFee(t *testing.T) {
	tests := []struct {
		name       string
		orderValue float64
		rb         float64
		want       float64
	}{
		{"percentage above floor", 1_000_000, 0.03, 30_000},
		{"100 ISK floor", 1_000, 0.03, fees.MinFee},
		{"exactly at floor", fees.MinFee / 0.03, 0.03, fees.MinFee},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fees.PlacementFee(tc.orderValue, tc.rb); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("PlacementFee(%v, %v) = %v, want %v", tc.orderValue, tc.rb, got, tc.want)
			}
		})
	}
}

func TestModifyFee(t *testing.T) {
	// Broker fee rate 3%, Advanced Broker Relations 5: relist fraction
	// 1-(0.50+0.06*5) = 0.20.
	const rb = 0.03
	const abr = 5
	tests := []struct {
		name     string
		oldValue float64
		newValue float64
		want     float64
	}{
		{"raise: discount term plus difference", 100_000, 120_000, 0.20*rb*120_000 + rb*(120_000-100_000)},
		{"lower: discount term only", 120_000, 100_000, 0.20 * rb * 100_000},
		{"floor applies to a tiny modify", 1, 2, fees.MinFee},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fees.ModifyFee(tc.oldValue, tc.newValue, rb, abr)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("ModifyFee(%v, %v, %v, %v) = %v, want %v", tc.oldValue, tc.newValue, rb, abr, got, tc.want)
			}
		})
	}
}
