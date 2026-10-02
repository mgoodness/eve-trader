package cli

import (
	"context"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// Allocation is AllocatedUniverse's full output (spec §10–§14; ticket #49):
// the buy side's funded and unfunded sets, the filter layer's excluded set,
// the sell plan's funded recommendations and reason-tagged pending entries,
// the AllocationParams the sell-first allocation ran against, and the live
// PilotFacts the run's fees and order limit were derived from. Warnings
// carries the pipeline's route-lookup warnings.
type Allocation struct {
	FundedBuys   []engine.BuyRecommendation
	UnfundedBuys []engine.BuyRecommendation
	Excluded     []engine.Excluded
	FundedSells  []engine.SellRecommendation
	Pending      []engine.Pending
	Params       engine.AllocationParams
	Facts        PilotFacts
	Warnings     []string
}

// AllocatedUniverse runs the whole pipeline and allocates (spec §10, §13):
// it ranks the buy survivors (RankedUniverse), reconciles the trading-stock
// ledger against live ESI reusing the run's one access token (spec §7;
// decision 6), builds the sell plan (spec §12), and reserves the resources
// the pilot's pre-existing open orders already commit before running the
// sell-first allocation (spec §13; ADR 0007). Sells claim an order-limit
// slot before any new buy; a sell that can't get one lands in Pending
// (order-limit-exhausted) rather than being dropped. AllocatedUniverse is
// the seam BuildResult assembles the output contract (ticket #23) from.
func AllocatedUniverse(ctx context.Context, cfg Config) (Allocation, error) {
	ranked, excluded, facts, warnings, err := RankedUniverse(ctx, cfg)
	if err != nil {
		return Allocation{}, err
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return Allocation{}, err
	}
	reconciledLots, notes, openOrders, err := reconcileLedgerWithPilotFacts(ctx, cfg, store, facts)
	if err != nil {
		return Allocation{}, err
	}

	// Sell recommendations price against the station best ask in the full
	// candidate universe (spec §7 step 1). Its route-lookup warnings are the
	// same ones RankedUniverse already surfaced from the cached feed, so
	// they are dropped here rather than reported twice.
	universe, _, err := CandidateUniverse(ctx, cfg)
	if err != nil {
		return Allocation{}, err
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

	reservedSlots, reservedBudget := reservedResources(openOrders)
	params := engine.AllocationParams{
		Budget:         cfg.Values.Budget,
		OrderLimit:     facts.OrderLimit,
		MinOrder:       cfg.Values.MinOrder,
		CaptureRate:    cfg.Values.CaptureRate,
		HorizonDays:    cfg.Values.HorizonDays,
		Broker:         facts.Fees.Broker,
		ReservedSlots:  reservedSlots,
		ReservedBudget: reservedBudget,
	}

	fundedSells, pendingSells, fundedBuys, unfundedBuys := engine.AllocateWithSells(sells, ranked, params)
	pending = append(pending, pendingSells...)

	return Allocation{
		FundedBuys:   fundedBuys,
		UnfundedBuys: unfundedBuys,
		Excluded:     excluded,
		FundedSells:  fundedSells,
		Pending:      pending,
		Params:       params,
		Facts:        facts,
		Warnings:     warnings,
	}, nil
}

// reservedResources computes the resources the pilot's currently-open
// orders already commit, before this run allocates anything (spec §13
// steps 1–2; ticket #49): every open order — buy or sell, any origin —
// costs one order-limit slot, and only open buy orders cost budget, at their
// current escrow price × volume_remain (already-paid broker fees are sunk,
// open sells carry no escrow). An unknown-outcome lot is absent from the
// orders snapshot, so it reserves nothing; its unresolved fate surfaces as
// Pending instead (ADR 0003).
func reservedResources(orders []engine.CharacterOrder) (slots int, budget float64) {
	slots = len(orders)
	for _, o := range orders {
		if o.IsBuyOrder {
			budget += o.Price * float64(o.VolumeRemain)
		}
	}
	return slots, budget
}
