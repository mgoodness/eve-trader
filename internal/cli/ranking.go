package cli

import (
	"context"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// RankedUniverse runs the whole filter funnel (HistoryFilteredUniverse) and
// then ranks the survivors by expected daily profit (spec §9), descending,
// at the run's configured capture rate (spec §13, cfg.Values.CaptureRate).
// This is the seam the allocation stage (#22) builds on.
func RankedUniverse(ctx context.Context, cfg Config) ([]engine.Recommendation, []engine.Excluded, PilotFacts, []string, error) {
	recs, excluded, facts, warnings, err := HistoryFilteredUniverse(ctx, cfg)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}

	return engine.Rank(recs, cfg.Values.CaptureRate), excluded, facts, warnings, nil
}
