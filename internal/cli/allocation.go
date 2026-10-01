package cli

import (
	"context"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// AllocatedUniverse runs the whole pipeline (RankedUniverse) and allocates
// the ranked survivors under the run's budget and the pilot's live,
// skill-derived order limit (spec §10; ticket #22): the funded set and the
// not-funded-by-budget set, kept separate from candidates the filter layer
// already excluded. It also returns the live PilotFacts the run's fees and
// order limit were derived from, so a caller building the output
// contract's Meta (ticket #23) doesn't trigger a second live ESI call for
// the same run. This is the seam the output contract (#23) builds on.
func AllocatedUniverse(ctx context.Context, cfg Config) (funded, unfunded []engine.Recommendation, excluded []engine.Excluded, facts PilotFacts, warnings []string, err error) {
	ranked, excluded, facts, warnings, err := RankedUniverse(ctx, cfg)
	if err != nil {
		return nil, nil, nil, PilotFacts{}, nil, err
	}

	params := engine.AllocationParams{
		Budget:      cfg.Values.Budget,
		OrderLimit:  facts.OrderLimit,
		MinOrder:    cfg.Values.MinOrder,
		CaptureRate: cfg.Values.CaptureRate,
		HorizonDays: cfg.Values.HorizonDays,
		Broker:      facts.Fees.Broker,
	}

	funded, unfunded = engine.Allocate(ranked, params)
	return funded, unfunded, excluded, facts, warnings, nil
}
