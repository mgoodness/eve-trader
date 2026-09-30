package cli

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// TwoSidedUniverse fetches the whole region feed (RegionFeed) and reports
// the two-sided universe (spec §6, §7 step 2): every type with both a
// covering best bid (the effective buy book) and a station best ask (the
// effective sell book), NPC-station locations only. It is the foundation
// later tickets build on: exact numeric jump-range coverage (#18) and the
// book-only/history-dependent filter funnel (#19, #20) both start from
// this set.
func TwoSidedUniverse(ctx context.Context, cfg Config) ([]engine.CandidateType, error) {
	orders, err := RegionFeed(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("fetching region feed: %w", err)
	}

	universe := engine.Universe(orders, cfg.TradeStationID, cfg.TradeSystemID)
	return engine.TwoSided(universe), nil
}
