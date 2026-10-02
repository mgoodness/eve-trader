package cli

import (
	"context"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// BuildResult runs the whole pipeline (AllocatedUniverse) and assembles the
// stable output contract (spec §11, §14; ticket #23): the funded buy set,
// the not-funded-by-budget set, and the excluded set, plus the sell plan
// built from the reconciled trading-stock ledger (spec §12), wrapped in a
// Meta that echoes the run's configured params and the pilot's live fees,
// and a Summary computed from the buy sets (engine.NewResult). This is what
// both the default table and --json renderers build on — the render layer
// performs no engine logic of its own.
func BuildResult(ctx context.Context, cfg Config) (engine.Result, []string, error) {
	funded, unfunded, excluded, facts, warnings, err := AllocatedUniverse(ctx, cfg)
	if err != nil {
		return engine.Result{}, nil, err
	}

	// Reconcile the trading-stock ledger against live ESI (spec §7), reusing
	// the run's already-minted access token rather than triggering a second
	// refresh-token exchange (decision 6).
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return engine.Result{}, nil, err
	}
	reconciledLots, notes, openOrders, err := reconcileLedgerWithPilotFacts(ctx, cfg, store, facts)
	if err != nil {
		return engine.Result{}, nil, err
	}

	// Sell recommendations price against the station best ask in the full
	// candidate universe (spec §7 step 1). Its route-lookup warnings are the
	// same ones AllocatedUniverse already surfaced from the cached feed, so
	// they are dropped here rather than reported twice.
	universe, _, err := CandidateUniverse(ctx, cfg)
	if err != nil {
		return engine.Result{}, nil, err
	}

	sells, pending, _ := engine.RecommendSells(engine.SellInputs{
		Lots:           reconciledLots,
		OpenOrders:     openOrders,
		Notes:          notes,
		Universe:       universe,
		TradeStationID: cfg.TradeStationID,
		Fees:           facts.Fees,
		Delta:          cfg.Values.Delta,
		TargetMargin:   cfg.Values.TargetMargin,
	})

	nameWarnings, err := populateNames(ctx, cfg, funded, unfunded, excluded, sells, pending)
	if err != nil {
		return engine.Result{}, nil, err
	}
	warnings = append(warnings, nameWarnings...)

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
			Accounting:      facts.Accounting,
			BrokerRelations: facts.BrokerRelations,
			FactionStanding: facts.FactionStanding,
			CorpStanding:    facts.CorpStanding,
		},
		Fees: facts.Fees,
	}

	return engine.NewResult(funded, unfunded, excluded, sells, pending, meta, facts.OrderLimit), warnings, nil
}
