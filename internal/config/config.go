// Package config resolves eve-trader's on-disk configuration and state
// surface (spec §13): documented defaults for the values a run needs
// (budget, target net margin, δ, horizon, minimum order, capture rate, and
// the filter thresholds), a TOML reader for the user's overrides at
// config.toml, the credentials file, and the config/cache directory
// resolution every other package builds its disk paths from.
//
// Skills, standings, fees, and the order limit are read live from ESI every
// run and are never part of Values (spec §13) — see internal/esi.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Values are every value a run needs that a pilot may reasonably want to
// tune (spec §13). Defaults returns the documented defaults; Load overlays
// a config.toml file's values on top of them; CLI flags (internal/cli)
// overlay those in turn.
type Values struct {
	Budget       int64            `toml:"budget"`
	TargetMargin float64          `toml:"target_margin"`
	Delta        float64          `toml:"delta"`
	HorizonDays  int              `toml:"horizon_days"`
	MinOrder     int64            `toml:"min_order"`
	CaptureRate  float64          `toml:"capture_rate"`
	Filters      FilterThresholds `toml:"filters"`
}

// FilterThresholds are the configurable cutoffs for the filter layer's
// book-only and history-dependent stages (spec §7).
type FilterThresholds struct {
	GrossMarginCeiling float64 `toml:"gross_margin_ceiling"`
	ThinBookMinOrders  int     `toml:"thin_book_min_orders"`
	ThinBookBandPct    float64 `toml:"thin_book_band_pct"`
	MinHistoryDays     int     `toml:"min_history_days"`
	MinLiquidityADV    float64 `toml:"min_liquidity_adv"`
	PriceBandLow       float64 `toml:"price_band_low"`
	PriceBandHigh      float64 `toml:"price_band_high"`
}

// Load reads a config.toml at path and overlays it on Defaults(): any field
// the file doesn't set keeps its documented default. A missing file is not
// an error — it returns Defaults() unchanged, so a fresh install runs with
// sane behaviour before the pilot has written a config.toml at all. That is
// the spec-consistent reading of §13 ("defaults live in config.toml") and of
// ticket #15's "documented default path": the first-run pilot has no file,
// so "missing" cannot be fatal. A present-but-malformed file is a clear error
// naming path and the decode failure; callers should treat that as fatal, not
// fall back silently.
func Load(path string) (Values, error) {
	values := Defaults()

	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return Values{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	if _, err := toml.DecodeFile(path, &values); err != nil {
		return Values{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return values, nil
}

// Defaults returns the documented built-in defaults (spec §7, §9, §10,
// CONTEXT.md "Aggression tick (δ)"): budget 150M ISK, target net margin 10%
// (undocumented by the spec itself; matches the closest prior-art default in
// docs/research/eveprofits-prior-art.md), δ 100 ISK, a 3-day capture horizon,
// a 1M ISK minimum order, a flat 20% capture rate, an 80% gross-margin
// ceiling, a thin-book test of 2 orders within a 5% band of best, 7 days of
// required history, a 20 units/day minimum ADV, and a 0.75×/1.25× 30-day
// low/high price band.
func Defaults() Values {
	return Values{
		Budget:       150_000_000,
		TargetMargin: 0.10,
		Delta:        100,
		HorizonDays:  3,
		MinOrder:     1_000_000,
		CaptureRate:  0.20,
		Filters: FilterThresholds{
			GrossMarginCeiling: 0.80,
			ThinBookMinOrders:  2,
			ThinBookBandPct:    0.05,
			MinHistoryDays:     7,
			MinLiquidityADV:    20,
			PriceBandLow:       0.75,
			PriceBandHigh:      1.25,
		},
	}
}
