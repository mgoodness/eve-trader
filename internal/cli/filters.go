package cli

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// BookFilteredUniverse runs the book-only filter stage (spec §7, §6
// "History funnel") over the whole two-sided universe (TwoSidedUniverse):
// gross-margin ceiling, thin book, then the pricing rule, in that order,
// using the pilot's live fees (ticket #16) and the run's configured
// thresholds (spec §13, cfg.Values.TargetMargin/Filters). No history call
// is made — engine.FilterBookOnly takes no history and this function fetches
// none. Any route-lookup warnings from the underlying jump-distance
// resolution (ticket #18) are returned alongside the result. This is the
// seam the history-dependent filters (#20) build on: they run only on the
// survivors this returns. It also returns the pilot's live facts (fees,
// order limit) read to do so, so later stages (allocation, ticket #22)
// don't trigger a second live ESI call for the same run.
//
// The pilot's own open orders are read here too, before TwoSidedUniverse
// builds the effective books (ticket #51; engine.Universe), so a type whose
// only competing bid or ask is the pilot's own order is never priced
// against itself. AllocatedUniverse's own direct CandidateUniverse call
// (sell pricing) rebuilds the same set separately -- it hits the warm
// 1,200s-TTL disk cache this fetch just populated rather than a second
// live ESI call.
func BookFilteredUniverse(ctx context.Context, cfg Config) ([]engine.BuyRecommendation, []engine.Excluded, PilotFacts, []string, error) {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}
	facts, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, fmt.Errorf("reading live pilot facts: %w", err)
	}
	ownOrderIDs, err := ownOrderIDSet(ctx, cfg, store, facts)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}

	twoSided, warnings, err := TwoSidedUniverse(ctx, cfg, ownOrderIDs)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}

	params := engine.Params{
		Delta:        cfg.Values.Delta,
		Fees:         facts.Fees,
		TargetMargin: cfg.Values.TargetMargin,
		Filters: engine.FilterThresholds{
			GrossMarginCeiling: cfg.Values.Filters.GrossMarginCeiling,
			ThinBookMinOrders:  cfg.Values.Filters.ThinBookMinOrders,
			ThinBookBandPct:    cfg.Values.Filters.ThinBookBandPct,
		},
	}

	recommendations, excluded := engine.FilterBookOnly(twoSided, params)
	return recommendations, excluded, facts, warnings, nil
}
