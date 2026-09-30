package cli

import (
	"context"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// BuildResult runs the whole pipeline (AllocatedUniverse) and assembles the
// stable output contract (spec §11; ticket #23): the funded set, the
// not-funded-by-budget set, and the excluded set, wrapped in a Meta that
// echoes the run's configured params and the pilot's live fees, and a
// Summary computed from the three sets (engine.NewResult). This is what
// both the default table and --json renderers build on — the render layer
// performs no engine logic of its own.
func BuildResult(ctx context.Context, cfg Config) (engine.Result, []string, error) {
	funded, unfunded, excluded, facts, warnings, err := AllocatedUniverse(ctx, cfg)
	if err != nil {
		return engine.Result{}, nil, err
	}

	meta := engine.Meta{
		GeneratedAt:  time.Now().UTC(),
		RegionID:     cfg.RegionID,
		TradeStation: cfg.TradeStationID,
		Params: engine.RunParams{
			Budget:          cfg.Values.Budget,
			TargetMargin:    cfg.Values.TargetMargin,
			Tick:            cfg.Values.Delta,
			HorizonDays:     cfg.Values.HorizonDays,
			CaptureRate:     cfg.Values.CaptureRate,
			Accounting:      facts.Accounting,
			BrokerRelations: facts.BrokerRelations,
			FactionStanding: facts.FactionStanding,
			CorpStanding:    facts.CorpStanding,
		},
		Fees: facts.Fees,
	}

	return engine.NewResult(funded, unfunded, excluded, meta, facts.OrderLimit), warnings, nil
}
