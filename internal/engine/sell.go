package engine

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

// SellRecommendation is one held type's sell order (spec §11, §14; ADR
// 0005): always the full held-unlisted quantity, priced at the current
// front-of-queue ask, with the §12 net margin. It bypasses the buy-side
// filter layer and EDP ranking. Field names and JSON tags match the output
// contract (spec §14).
type SellRecommendation struct {
	TypeID    int32   `json:"type_id"`
	Name      string  `json:"name"`
	Quantity  int64   `json:"quantity"`
	SellPrice float64 `json:"sell_price"`
	NetMargin float64 `json:"net_margin"`

	// BelowTarget is the margin-gate flag (spec §12; ADR 0006): a margin
	// under the configured target flags the recommendation without ever
	// excluding or hiding it. A recommendation with no priced quantity
	// (every lot seeded, ADR 0004) is never flagged.
	BelowTarget bool `json:"below_target"`

	// PricedQuantity is how many units the shown NetMargin covers; the
	// remainder, UnpricedQuantity, comes from seeded lots with no known
	// acquisition price (spec §12, §7).
	PricedQuantity   int64 `json:"priced_quantity"`
	UnpricedQuantity int64 `json:"unpriced_quantity"`

	Lots []SellLot `json:"lots"`

	// NetProceeds is gross revenue less sales tax and this order's own
	// broker fee — never netted against acquisition price, which is net
	// margin's job (spec §14).
	NetProceeds float64 `json:"net_proceeds"`
}

// SellLot is one ledger lot contributing to a SellRecommendation (spec
// §14): its id, the quantity offered, and its acquisition price (nil for a
// seeded lot, ADR 0004).
type SellLot struct {
	LotID            string   `json:"lot_id"`
	Quantity         int64    `json:"quantity"`
	AcquisitionPrice *float64 `json:"acquisition_price"`
}

// Pending is the one reason-tagged bucket for everything in flight and
// non-actionable this run (spec §11, §14): an open-buy lot, an open sell
// order already covering stock, or a lot whose fill outcome ESI cannot
// resolve. It mirrors the excluded bucket's count-by-default/detail-under-
// --explain shape and never counts toward funded, unfunded, excluded, or
// sell recommendations.
type Pending struct {
	TypeID   int32  `json:"type_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Quantity int64  `json:"quantity"`
	OrderID  int64  `json:"order_id,omitempty"`
	Detail   string `json:"detail"`
}

// The pending reasons (spec §11). PendingOrderLimit is reserved for the
// allocation stage (#49); the sell-side builder never emits it.
const (
	PendingAwaitingBuyFill  = "awaiting-buy-fill"
	PendingAwaitingSellFill = "awaiting-sell-fill"
	PendingUnknownOutcome   = "unknown-outcome"
	PendingOrderLimit       = "order-limit-exhausted"
)

// Float64Ptr returns a pointer to v, for building acquisition prices in
// literals and tests.
func Float64Ptr(v float64) *float64 { return &v }

// SellInputs are RecommendSells' pure inputs: the reconciled ledger, the
// open character orders used to reserve held stock already covered by a
// sell order, the reconciliation notes that carry unknown-outcome reports,
// the candidate universe used for each type's station best ask, the trade
// station id, and the run's live fees, δ, and target margin.
type SellInputs struct {
	Lots           []Lot
	OpenOrders     []CharacterOrder
	Notes          []ReconcileNote
	Universe       []CandidateType
	TradeStationID int64
	Fees           Fees
	Delta          float64
	TargetMargin   float64
}

// RecommendSells builds the run's sell recommendations and pending entries
// (spec §11, §12; ADR 0005, 0006). It is pure: no clock, no network, no
// disk. One recommendation is produced per type with held-unlisted stock,
// for the full unreserved quantity, priced at bestAsk − δ. It bypasses the
// buy-side filters and ranking entirely; a margin under target is flagged,
// never excluded. A type with no station sell order has no front-of-queue
// price and is skipped (the lot stays held-unlisted).
//
// The returned updated lots copy the input with reservations applied: a
// lot fully covered by open station sell orders becomes reserved-for-sale,
// and a partly covered one keeps held-unlisted status with its availability
// reduced to the unreserved remainder. The caller decides whether to
// persist them; RecommendSells itself writes nothing.
func RecommendSells(in SellInputs) (recs []SellRecommendation, pending []Pending, updated []Lot) {
	updated = make([]Lot, len(in.Lots))
	copy(updated, in.Lots)

	pending = append(pending, awaitingBuyFillPending(updated)...)
	pending = append(pending, reserveForSale(updated, in.OpenOrders, in.TradeStationID)...)
	pending = append(pending, unknownOutcomePending(in.Notes, updated)...)

	bestAsk := make(map[int32]float64, len(in.Universe))
	for _, c := range in.Universe {
		if ask, ok := bestPrice(c.SellBook); ok {
			bestAsk[c.TypeID] = ask
		}
	}

	// Collect each held-unlisted type's lots in ledger order (the ledger
	// keeps lots in acquisition order, so this is FIFO).
	byType := make(map[int32][]int)
	var typeIDs []int32
	for i := range updated {
		lot := updated[i]
		if lot.Status != LotHeldUnlisted || lot.QuantityAvailable <= 0 {
			continue
		}
		if _, seen := byType[lot.TypeID]; !seen {
			typeIDs = append(typeIDs, lot.TypeID)
		}
		byType[lot.TypeID] = append(byType[lot.TypeID], i)
	}
	sort.Slice(typeIDs, func(i, j int) bool { return typeIDs[i] < typeIDs[j] })

	for _, typeID := range typeIDs {
		ask, ok := bestAsk[typeID]
		if !ok {
			continue // no station sell order: no front-of-queue price (decision 7)
		}
		recs = append(recs, buildSellRecommendation(typeID, byType[typeID], updated, ask, in))
	}

	sortSellRecommendations(recs, in.TargetMargin)
	return recs, pending, updated
}

// buildSellRecommendation folds one type's held-unlisted lots into a single
// SellRecommendation (spec §11, §12): one recommendation, one sell order,
// with the margin a quantity-weighted average over the priced lots.
func buildSellRecommendation(typeID int32, lotIndexes []int, lots []Lot, bestAsk float64, in SellInputs) SellRecommendation {
	sellPrice := bestAsk - in.Delta

	var quantity, pricedQuantity int64
	var weightedCost float64
	sellLots := make([]SellLot, 0, len(lotIndexes))
	for _, i := range lotIndexes {
		lot := lots[i]
		sellLots = append(sellLots, SellLot{
			LotID:            lot.LotID,
			Quantity:         lot.QuantityAvailable,
			AcquisitionPrice: lot.AcquisitionPrice,
		})
		quantity += lot.QuantityAvailable
		if lot.AcquisitionPrice != nil {
			pricedQuantity += lot.QuantityAvailable
			weightedCost += *lot.AcquisitionPrice * float64(lot.QuantityAvailable)
		}
	}

	var netMargin float64
	if pricedQuantity > 0 {
		acquisition := weightedCost / float64(pricedQuantity)
		netMargin = (sellPrice - acquisition - in.Fees.Broker*acquisition -
			in.Fees.Broker*sellPrice - in.Fees.SalesTax*sellPrice) / sellPrice
	}

	return SellRecommendation{
		TypeID:           typeID,
		Quantity:         quantity,
		SellPrice:        sellPrice,
		NetMargin:        netMargin,
		BelowTarget:      pricedQuantity > 0 && netMargin < in.TargetMargin,
		PricedQuantity:   pricedQuantity,
		UnpricedQuantity: quantity - pricedQuantity,
		Lots:             sellLots,
		NetProceeds:      float64(quantity) * sellPrice * (1 - in.Fees.Broker - in.Fees.SalesTax),
	}
}

// sortSellRecommendations sorts worst margin shortfall first (spec §11): a
// larger shortfall (target − margin) sorts earlier. A recommendation with no
// priced quantity has no measurable shortfall and sorts last; ties break by
// ascending type id so the order is deterministic.
func sortSellRecommendations(recs []SellRecommendation, targetMargin float64) {
	sort.SliceStable(recs, func(i, j int) bool {
		si, sj := sellShortfall(recs[i], targetMargin), sellShortfall(recs[j], targetMargin)
		if si != sj {
			return si > sj
		}
		return recs[i].TypeID < recs[j].TypeID
	})
}

// sellShortfall is target − margin for a priced recommendation, and −Inf for
// one with no priced quantity (unknown margin sorts last).
func sellShortfall(rec SellRecommendation, targetMargin float64) float64 {
	if rec.PricedQuantity == 0 {
		return math.Inf(-1)
	}
	return targetMargin - rec.NetMargin
}

// awaitingBuyFillPending reports one Pending per open-buy lot, the units
// still unfilled being the quantity (spec §11).
func awaitingBuyFillPending(lots []Lot) []Pending {
	var pending []Pending
	for i := range lots {
		lot := lots[i]
		if lot.Status != LotOpenBuy {
			continue
		}
		orderID, _ := strconv.ParseInt(lot.SourceOrderID, 10, 64)
		pending = append(pending, Pending{
			TypeID:   lot.TypeID,
			Reason:   PendingAwaitingBuyFill,
			Quantity: lot.LastSeenVolumeRemain,
			OrderID:  orderID,
			Detail: fmt.Sprintf("buy order %s awaiting fill (%d of %d units remaining)",
				lot.SourceOrderID, lot.LastSeenVolumeRemain, lot.QuantityTotal),
		})
	}
	return pending
}

// unknownOutcomePending reports one Pending per unknown-outcome
// reconciliation note (spec §7, §11; ADR 0003): an order that vanished
// without a cancelled/expired record, so its last-seen remainder is
// surfaced rather than guessed filled or cancelled.
func unknownOutcomePending(notes []ReconcileNote, lots []Lot) []Pending {
	byID := make(map[string]*Lot, len(lots))
	for i := range lots {
		byID[lots[i].LotID] = &lots[i]
	}

	var pending []Pending
	for _, note := range notes {
		if note.Kind != NoteUnknown {
			continue
		}
		var quantity int64
		if lot, ok := byID[note.LotID]; ok {
			quantity = lot.LastSeenVolumeRemain
		}
		pending = append(pending, Pending{
			TypeID:   note.TypeID,
			Reason:   PendingUnknownOutcome,
			Quantity: quantity,
			OrderID:  note.OrderID,
			Detail:   note.Detail,
		})
	}
	return pending
}

// reserveForSale reserves held-unlisted stock already covered by an open
// sell order at the trade station (spec §11; ADR 0005), taking units FIFO
// from the type's held lots up to the open orders' combined remaining volume
// and reporting one awaiting-sell-fill Pending per type with reserved stock.
// A lot fully covered becomes reserved-for-sale and drops out of the sell
// plan; a lot only partly covered keeps its held-unlisted status with its
// availability reduced to the unreserved remainder, so only the truly
// unreserved units are recommended — never more than the pilot holds. Types
// are visited in ascending type-id order so the pending list is
// deterministic.
func reserveForSale(lots []Lot, orders []CharacterOrder, tradeStationID int64) []Pending {
	type openSell struct {
		remaining    int64
		firstOrderID int64
	}
	open := make(map[int32]*openSell)
	for _, o := range orders {
		if o.IsBuyOrder || o.LocationID != tradeStationID {
			continue
		}
		os, ok := open[o.TypeID]
		if !ok {
			os = &openSell{firstOrderID: o.OrderID}
			open[o.TypeID] = os
		}
		os.remaining += o.VolumeRemain
	}
	if len(open) == 0 {
		return nil
	}

	var typeIDs []int32
	for typeID := range open {
		typeIDs = append(typeIDs, typeID)
	}
	sort.Slice(typeIDs, func(i, j int) bool { return typeIDs[i] < typeIDs[j] })

	var pending []Pending
	for _, typeID := range typeIDs {
		os := open[typeID]
		remaining := os.remaining
		if remaining <= 0 {
			continue
		}

		var reservedQuantity int64
		for i := range lots {
			if remaining <= 0 {
				break
			}
			lot := &lots[i]
			if lot.TypeID != typeID || lot.Status != LotHeldUnlisted || lot.QuantityAvailable <= 0 {
				continue
			}
			take := remaining
			if take > lot.QuantityAvailable {
				take = lot.QuantityAvailable
			}
			lot.QuantityAvailable -= take
			if lot.QuantityAvailable == 0 {
				lot.Status = LotReservedForSale
			}
			remaining -= take
			reservedQuantity += take
		}

		if reservedQuantity > 0 {
			pending = append(pending, Pending{
				TypeID:   typeID,
				Reason:   PendingAwaitingSellFill,
				Quantity: reservedQuantity,
				OrderID:  os.firstOrderID,
				Detail: fmt.Sprintf("open sell order %d reserves %d held units",
					os.firstOrderID, reservedQuantity),
			})
		}
	}
	return pending
}
