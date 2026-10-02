package cli

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
)

// Allocation is AllocatedUniverse's full output (spec §10–§14; ticket #49):
// the buy side's funded and unfunded sets, the filter layer's excluded set,
// the sell plan's funded recommendations and reason-tagged pending entries,
// the AllocationParams the sell-first allocation ran against, and the live
// PilotFacts the run's fees and order limit were derived from. Warnings
// carries the route-lookup warnings plus the reconciliation outcomes and
// sell-plan skips worth a pilot's attention.
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

	// The reconciled ledger already carries this run's reservations and sell
	// finalizations (engine.Reconcile persisted them via
	// reconcileLedgerWithPilotFacts), so RecommendSells only reads them.
	sells, pending, sellWarnings := engine.RecommendSells(engine.SellInputs{
		Lots:         reconciledLots,
		Notes:        notes,
		Universe:     universe,
		Fees:         facts.Fees,
		Delta:        cfg.Values.Delta,
		TargetMargin: cfg.Values.TargetMargin,
	})

	reservedSlots, reservedBudget := engine.ReservedResources(openOrders)
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

	// Reconciliation outcomes worth a pilot's attention on every run (spec §7;
	// ADR 0003), plus held types the sell plan had to skip, go to stderr via
	// the returned warnings.
	warnings = append(warnings, reconcileWarnings(notes)...)
	warnings = append(warnings, sellWarnings...)

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

// reconcileWarnings renders the reconciliation outcomes worth surfacing on
// the recommend path (spec §7; ADR 0003): a drift clamp, a released
// reservation, an unresolvable outcome, or a completed sale. Routine buy-side
// partial and full fills are not warnings.
func reconcileWarnings(notes []engine.ReconcileNote) []string {
	var warnings []string
	for _, n := range notes {
		switch n.Kind {
		case engine.NoteDriftClamp, engine.NoteTerminal, engine.NoteUnknown, engine.NoteSold:
			warnings = append(warnings, fmt.Sprintf("warning: %s", n.Detail))
		}
	}
	return warnings
}
