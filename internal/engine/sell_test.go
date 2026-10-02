package engine_test

import (
	"math"
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// heldLot builds a held-unlisted lot for the sell-side tests.
func heldLot(lotID string, typeID int32, qty int64, price *float64) engine.Lot {
	return engine.Lot{
		LotID:             lotID,
		TypeID:            typeID,
		QuantityTotal:     qty,
		QuantityAvailable: qty,
		AcquisitionPrice:  price,
		Status:            engine.LotHeldUnlisted,
	}
}

// stationAsk builds a candidate universe entry whose station sell book
// holds a single order at price.
func stationAsk(typeID int32, price float64) engine.CandidateType {
	return engine.CandidateType{
		TypeID: typeID,
		SellBook: []engine.Order{{
			OrderID:      1,
			TypeID:       typeID,
			LocationID:   60004588,
			IsBuyOrder:   false,
			Price:        price,
			VolumeRemain: 1000,
		}},
	}
}

// TestRecommendSellsPricesAgainstTheAcquisitionPriceAndTheStationAsk pins
// the §12 margin formula: S* = best ask − δ, B = the lot's acquisition
// price, and the net margin is (S* − B − broker·B − broker·S* − tax·S*)/S*.
// The expected value is computed here from that formula, independently of
// the implementation.
func TestRecommendSellsPricesAgainstTheAcquisitionPriceAndTheStationAsk(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(12.5))}
	universe := []engine.CandidateType{stationAsk(34, 20)}
	fees := engine.Fees{Broker: 0.018, SalesTax: 0.05025}
	const delta = 0.5
	const target = 0.10

	recs, pending, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     universe,
		Fees:         fees,
		Delta:        delta,
		TargetMargin: target,
	})

	if len(pending) != 0 {
		t.Fatalf("got pending %+v, want none", pending)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d sell recommendations, want 1", len(recs))
	}

	rec := recs[0]
	s := 20.0 - delta
	b := 12.5
	wantMargin := (s - b - fees.Broker*b - fees.Broker*s - fees.SalesTax*s) / s
	if math.Abs(rec.NetMargin-wantMargin) > 1e-9 {
		t.Errorf("got NetMargin=%v, want %v (§12)", rec.NetMargin, wantMargin)
	}
	if rec.SellPrice != s {
		t.Errorf("got SellPrice=%v, want %v (best ask - delta)", rec.SellPrice, s)
	}
	if rec.Quantity != 100 {
		t.Errorf("got Quantity=%d, want the full held quantity 100", rec.Quantity)
	}
	if rec.PricedQuantity != 100 || rec.UnpricedQuantity != 0 {
		t.Errorf("got priced=%d unpriced=%d, want 100/0", rec.PricedQuantity, rec.UnpricedQuantity)
	}
	if rec.BelowTarget {
		t.Errorf("got BelowTarget=true for a %.1f%% margin over a %.1f%% target", rec.NetMargin*100, target*100)
	}
	wantProceeds := 100 * s * (1 - fees.Broker - fees.SalesTax)
	if math.Abs(rec.NetProceeds-wantProceeds) > 1e-9 {
		t.Errorf("got NetProceeds=%v, want %v (gross less tax and broker)", rec.NetProceeds, wantProceeds)
	}
	if len(rec.Lots) != 1 || rec.Lots[0].LotID != "lot-1" || rec.Lots[0].Quantity != 100 {
		t.Errorf("got lots %+v, want the one source lot with its full quantity", rec.Lots)
	}
}

// TestRecommendSellsFlagsBelowTargetWithoutExcluding pins the margin gate
// (spec §12; ADR 0006): a margin under target sets BelowTarget but the
// recommendation is still produced.
func TestRecommendSellsFlagsBelowTargetWithoutExcluding(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(19))}
	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{stationAsk(34, 20)},
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	if len(recs) != 1 {
		t.Fatalf("got %d sell recommendations, want the held type still recommended despite the margin", len(recs))
	}
	if !recs[0].BelowTarget {
		t.Errorf("got BelowTarget=false for margin %v under a 5%% target", recs[0].NetMargin)
	}
}

// TestRecommendSellsBlendsAMultiLotsAcquisitionPriceAsAQuantityWeightedAverage
// pins spec §12's multi-lot blending: one recommendation, one number, the
// quantity-weighted average of the priced lots' acquisition prices.
func TestRecommendSellsBlendsAMultiLotsAcquisitionPriceAsAQuantityWeightedAverage(t *testing.T) {
	lots := []engine.Lot{
		heldLot("lot-a", 34, 100, engine.Float64Ptr(10)),
		heldLot("lot-b", 34, 300, engine.Float64Ptr(14)),
	}
	fees := engine.Fees{Broker: 0.018, SalesTax: 0.05025}
	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{stationAsk(34, 30)},
		Fees:         fees,
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	if len(recs) != 1 {
		t.Fatalf("got %d sell recommendations, want 1 per type", len(recs))
	}
	rec := recs[0]
	if rec.Quantity != 400 {
		t.Errorf("got Quantity=%d, want the full 400 held units", rec.Quantity)
	}
	s := 30.0 - 0.5
	acquisition := (100.0*10 + 300.0*14) / 400.0
	wantMargin := (s - acquisition - fees.Broker*acquisition - fees.Broker*s - fees.SalesTax*s) / s
	if math.Abs(rec.NetMargin-wantMargin) > 1e-9 {
		t.Errorf("got NetMargin=%v, want the weighted-average %v", rec.NetMargin, wantMargin)
	}
	if len(rec.Lots) != 2 {
		t.Errorf("got %d lots, want both contributing lots", len(rec.Lots))
	}
}

// TestRecommendSellsSurfacesUnpricedSeededQuantity pins spec §12's seeded-
// lot handling: the margin covers only the priced quantity and the
// unpriced remainder is stated, never guessed at or dropped.
func TestRecommendSellsSurfacesUnpricedSeededQuantity(t *testing.T) {
	lots := []engine.Lot{
		heldLot("lot-a", 34, 630, engine.Float64Ptr(10)),
		heldLot("lot-seeded", 34, 350, nil),
	}
	fees := engine.Fees{Broker: 0.018, SalesTax: 0.05025}
	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{stationAsk(34, 30)},
		Fees:         fees,
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	rec := recs[0]
	if rec.PricedQuantity != 630 || rec.UnpricedQuantity != 350 {
		t.Errorf("got priced=%d unpriced=%d, want 630/350", rec.PricedQuantity, rec.UnpricedQuantity)
	}
	if rec.Quantity != 980 {
		t.Errorf("got Quantity=%d, want the full 980 held units", rec.Quantity)
	}
	s := 29.5
	wantMargin := (s - 10 - fees.Broker*10 - fees.Broker*s - fees.SalesTax*s) / s
	if math.Abs(rec.NetMargin-wantMargin) > 1e-9 {
		t.Errorf("got NetMargin=%v, want the margin computed over the 630 priced units only (%v)", rec.NetMargin, wantMargin)
	}
}

// TestRecommendSellsNeverFlagsAGaplesslyPricedSeededRecommendation pins
// decision 9: a recommendation with no priced lot carries no margin (0) and
// is never flagged below target.
func TestRecommendSellsNeverFlagsAGaplesslyPricedSeededRecommendation(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-seeded", 34, 100, nil)}
	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{stationAsk(34, 20)},
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.20,
	})

	rec := recs[0]
	if rec.NetMargin != 0 {
		t.Errorf("got NetMargin=%v, want 0 with no priced quantity", rec.NetMargin)
	}
	if rec.BelowTarget {
		t.Errorf("got BelowTarget=true, want a seeded-only recommendation never flagged")
	}
	if rec.PricedQuantity != 0 || rec.UnpricedQuantity != 100 {
		t.Errorf("got priced=%d unpriced=%d, want 0/100", rec.PricedQuantity, rec.UnpricedQuantity)
	}
}

// TestRecommendSellsSkipsATypeWithNoStationSellOrder pins decision 7: a held
// type with no station sell order has no front-of-queue price and is skipped.
func TestRecommendSellsSkipsATypeWithNoStationSellOrder(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(10))}
	recs, _, updated := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{{TypeID: 34}}, // no sell book
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	if len(recs) != 0 {
		t.Errorf("got %+v, want no recommendation without a station sell order", recs)
	}
	if updated[0].Status != engine.LotHeldUnlisted {
		t.Errorf("got status %q, want the lot left held-unlisted", updated[0].Status)
	}
}

// TestRecommendSellsBypassesTheFilterLayerForAnEmptyBuyBook proves the sell
// side is independent of the buy pipeline: a type with no covering bid at
// all still earns a sell recommendation.
func TestRecommendSellsBypassesTheFilterLayerForAnEmptyBuyBook(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(10))}
	universe := []engine.CandidateType{{TypeID: 34, SellBook: stationAsk(34, 20).SellBook}}

	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     universe,
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	if len(recs) != 1 || recs[0].TypeID != 34 {
		t.Fatalf("got %+v, want type 34 recommended even with no buy book", recs)
	}
}

// TestRecommendSellsSortsWorstMarginShortfallFirst pins spec §11's ordering.
func TestRecommendSellsSortsWorstMarginShortfallFirst(t *testing.T) {
	lots := []engine.Lot{
		heldLot("lot-good", 1, 10, engine.Float64Ptr(2)),
		heldLot("lot-bad", 2, 10, engine.Float64Ptr(19)),
		heldLot("lot-mid", 3, 10, engine.Float64Ptr(12)),
	}
	recs, _, _ := engine.RecommendSells(engine.SellInputs{
		Lots: lots,
		Universe: []engine.CandidateType{
			stationAsk(1, 20),
			stationAsk(2, 20),
			stationAsk(3, 20),
		},
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	got := []int32{recs[0].TypeID, recs[1].TypeID, recs[2].TypeID}
	want := []int32{2, 3, 1} // worst shortfall (type 2) first
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got sell order %v, want %v (worst margin shortfall first)", got, want)
	}
}

// TestRecommendSellsReportsAnOpenBuyLotAsAwaitingFill pins the
// awaiting-buy-fill pending entry (spec §11).
func TestRecommendSellsReportsAnOpenBuyLotAsAwaitingFill(t *testing.T) {
	lots := []engine.Lot{{
		LotID:                "lot-open",
		TypeID:               34,
		SourceOrderID:        "1001",
		QuantityTotal:        100,
		QuantityAvailable:    0,
		Status:               engine.LotOpenBuy,
		LastSeenVolumeRemain: 60,
	}}

	recs, pending, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Universe:     []engine.CandidateType{stationAsk(34, 20)},
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	if len(recs) != 0 {
		t.Errorf("got %+v, want no recommendation for an open-buy lot", recs)
	}
	if len(pending) != 1 {
		t.Fatalf("got pending %+v, want one awaiting-buy-fill entry", pending)
	}
	if pending[0].Reason != engine.PendingAwaitingBuyFill || pending[0].Quantity != 60 || pending[0].OrderID != 1001 {
		t.Errorf("got pending %+v, want awaiting-buy-fill 60 units on order 1001", pending[0])
	}
}

// TestRecommendSellsReportsAnUnknownOutcomeFromAReconcileNote pins pending
// decision 8: an unknown-outcome note surfaces the order id and last-seen
// remainder rather than guessing filled or cancelled.
func TestRecommendSellsReportsAnUnknownOutcomeFromAReconcileNote(t *testing.T) {
	lots := []engine.Lot{{
		LotID:                "lot-unknown",
		TypeID:               34,
		SourceOrderID:        "1002",
		QuantityTotal:        100,
		LastSeenVolumeRemain: 40,
		Status:               engine.LotOpenBuy,
	}}
	notes := []engine.ReconcileNote{{
		LotID:   "lot-unknown",
		TypeID:  34,
		OrderID: 1002,
		Kind:    engine.NoteUnknown,
		Detail:  "order 1002 vanished with 40 units unaccounted for",
	}}

	_, pending, _ := engine.RecommendSells(engine.SellInputs{
		Lots:         lots,
		Notes:        notes,
		Universe:     []engine.CandidateType{stationAsk(34, 20)},
		Fees:         engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:        0.5,
		TargetMargin: 0.05,
	})

	var unknown *engine.Pending
	for i := range pending {
		if pending[i].Reason == engine.PendingUnknownOutcome {
			unknown = &pending[i]
		}
	}
	if unknown == nil {
		t.Fatalf("got pending %+v, want an unknown-outcome entry", pending)
	}
	if unknown.Quantity != 40 || unknown.OrderID != 1002 || unknown.Detail == "" {
		t.Errorf("got unknown-outcome %+v, want quantity 40, order 1002, and a detail", *unknown)
	}
}

// TestRecommendSellsReservesHeldStockAlreadyCoveredByAnOpenSellOrder pins
// awaiting-sell-fill (spec §11): an open station sell order covering a
// held type reserves that stock, marks the lots reserved-for-sale, and
// suppresses the sell recommendation.
func TestRecommendSellsReservesHeldStockAlreadyCoveredByAnOpenSellOrder(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(10))}
	orders := []engine.CharacterOrder{{
		OrderID:      5000,
		TypeID:       34,
		LocationID:   60004588,
		IsBuyOrder:   false,
		VolumeRemain: 100,
	}}

	recs, pending, updated := engine.RecommendSells(engine.SellInputs{
		Lots:           lots,
		OpenOrders:     orders,
		Universe:       []engine.CandidateType{stationAsk(34, 20)},
		TradeStationID: 60004588,
		Fees:           engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:          0.5,
		TargetMargin:   0.05,
	})

	if len(recs) != 0 {
		t.Errorf("got %+v, want no recommendation for stock reserved by an open sell order", recs)
	}
	if len(pending) != 1 || pending[0].Reason != engine.PendingAwaitingSellFill || pending[0].Quantity != 100 || pending[0].OrderID != 5000 {
		t.Fatalf("got pending %+v, want awaiting-sell-fill 100 units on order 5000", pending)
	}
	if updated[0].Status != engine.LotReservedForSale {
		t.Errorf("got status %q, want reserved-for-sale", updated[0].Status)
	}
}

// TestRecommendSellsReservesOnlyTheUnitsAnOpenOrderCovers pins the FIFO
// reservation's unit granularity: an open order that covers only part of a
// lot reserves those units and reports them as awaiting-sell-fill, while
// the unreserved remainder still earns a sell recommendation — never more
// than the pilot holds.
func TestRecommendSellsReservesOnlyTheUnitsAnOpenOrderCovers(t *testing.T) {
	lots := []engine.Lot{heldLot("lot-1", 34, 100, engine.Float64Ptr(10))}
	orders := []engine.CharacterOrder{{
		OrderID:      5000,
		TypeID:       34,
		LocationID:   60004588,
		IsBuyOrder:   false,
		VolumeRemain: 60,
	}}

	recs, pending, updated := engine.RecommendSells(engine.SellInputs{
		Lots:           lots,
		OpenOrders:     orders,
		Universe:       []engine.CandidateType{stationAsk(34, 20)},
		TradeStationID: 60004588,
		Fees:           engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Delta:          0.5,
		TargetMargin:   0.05,
	})

	if len(pending) != 1 || pending[0].Reason != engine.PendingAwaitingSellFill || pending[0].Quantity != 60 {
		t.Fatalf("got pending %+v, want awaiting-sell-fill for the 60 reserved units", pending)
	}
	if len(recs) != 1 || recs[0].Quantity != 40 {
		t.Errorf("got recs=%+v, want the 40 unreserved units still recommended", recs)
	}
	if updated[0].Status != engine.LotHeldUnlisted || updated[0].QuantityAvailable != 40 {
		t.Errorf("got %+v, want the lot held-unlisted with only the 40 unreserved units available", updated[0])
	}
}
