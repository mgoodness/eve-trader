package cli

import (
	"context"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// BuildResult runs the whole pipeline (AllocatedUniverse) and assembles the
// stable output contract (spec §11, §14; ticket #23): the funded buy set,
// the not-funded-by-budget set, and the excluded set, plus the sell plan
// built from the reconciled trading-stock ledger (spec §12) and its pending
// bucket (including any sell that lost the order-limit contention, ticket
// #49), wrapped in a Meta that echoes the run's configured params and the
// pilot's live fees, and a Summary computed from the buy and sell sets and
// the allocation's resource split (engine.NewResult). This is what both the
// default table and --json renderers build on — the render layer performs
// no engine logic of its own.
func BuildResult(ctx context.Context, cfg Config) (engine.Result, []string, error) {
	alloc, err := AllocatedUniverse(ctx, cfg)
	if err != nil {
		return engine.Result{}, nil, err
	}

	nameWarnings, err := populateNames(ctx, cfg, alloc.FundedBuys, alloc.UnfundedBuys, alloc.Excluded, alloc.FundedSells, alloc.Pending)
	if err != nil {
		return engine.Result{}, nil, err
	}
	warnings := append(alloc.Warnings, nameWarnings...)

	meta := engine.Meta{
		GeneratedAt:  time.Now().UTC(),
		RegionID:     cfg.RegionID,
		TradeStation: cfg.TradeStationID,
		Params: engine.RunParams{
			Budget:          cfg.Values.Budget,
			TargetMargin:    cfg.Values.TargetMargin,
			Delta:           cfg.Values.Delta,
			HorizonDays:     cfg.Values.HorizonDays,
			CaptureRate:     cfg.Values.CaptureRate,
			Accounting:      alloc.Facts.Accounting,
			BrokerRelations: alloc.Facts.BrokerRelations,
			FactionStanding: alloc.Facts.FactionStanding,
			CorpStanding:    alloc.Facts.CorpStanding,
		},
		Fees: alloc.Facts.Fees,
	}

	result := engine.NewResult(alloc.FundedBuys, alloc.UnfundedBuys, alloc.Excluded, alloc.FundedSells, alloc.Pending, meta, alloc.Params)
	return result, warnings, nil
}
