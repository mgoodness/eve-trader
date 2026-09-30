package cli

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// TwoSidedUniverse fetches the whole region feed (RegionFeed) and reports
// the two-sided universe (spec §6, §7 step 2): every type with both a
// covering best bid (the effective buy book) and a station best ask (the
// effective sell book), NPC-station locations only. Numeric-range buy
// orders are evaluated against an exact jump distance, resolved and cached
// by JumpDistances (ticket #18); any warnings it records (failed route
// lookups) are returned alongside the universe. It is the foundation later
// tickets build on: the book-only/history-dependent filter funnel (#19,
// #20) starts from this set.
func TwoSidedUniverse(ctx context.Context, cfg Config) ([]engine.CandidateType, []string, error) {
	orders, err := RegionFeed(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching region feed: %w", err)
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, nil, err
	}
	jumpDistances, warnings := JumpDistances(ctx, cfg, store, orders)

	universe := engine.Universe(orders, cfg.TradeStationID, cfg.TradeSystemID, jumpDistances)
	return engine.TwoSided(universe), warnings, nil
}
