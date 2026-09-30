package engine

import "sort"

// CandidateType is one type's place in the candidate universe (spec §7 step
// 1, CONTEXT.md "candidate universe"): the types that have a live Heimatar
// order, paired with their effective books at the trade station (spec §3).
type CandidateType struct {
	TypeID   int32
	SellBook []Order
	BuyBook  []Order
}

// Universe groups a region-orders feed by type and computes each type's
// effective sell and buy books (spec §3, §6). Every type with at least one
// region order gets an entry, in ascending type_id order, even if one side
// of its book is empty. jumpDistances is forwarded to EffectiveBuyBook
// unchanged for numeric-range coverage (ticket #18); pass nil if the
// caller has none (no numeric-range order will cover).
func Universe(orders []Order, tradeStationID int64, tradeSystemID int32, jumpDistances map[int32]int) []CandidateType {
	byType := make(map[int32][]Order)
	var typeIDs []int32
	for _, o := range orders {
		if _, ok := byType[o.TypeID]; !ok {
			typeIDs = append(typeIDs, o.TypeID)
		}
		byType[o.TypeID] = append(byType[o.TypeID], o)
	}
	sort.Slice(typeIDs, func(i, j int) bool { return typeIDs[i] < typeIDs[j] })

	universe := make([]CandidateType, 0, len(typeIDs))
	for _, typeID := range typeIDs {
		typeOrders := byType[typeID]
		universe = append(universe, CandidateType{
			TypeID:   typeID,
			SellBook: EffectiveSellBook(typeOrders, tradeStationID),
			BuyBook:  EffectiveBuyBook(typeOrders, tradeStationID, tradeSystemID, jumpDistances),
		})
	}
	return universe
}

// TwoSided reports the subset of universe whose books clear the two-sided
// filter (spec §7 step 2): a covering best bid (the effective buy book) and
// a station best ask (the effective sell book).
func TwoSided(universe []CandidateType) []CandidateType {
	var twoSided []CandidateType
	for _, c := range universe {
		if _, ok := bestPrice(c.BuyBook); !ok {
			continue
		}
		if _, ok := bestPrice(c.SellBook); !ok {
			continue
		}
		twoSided = append(twoSided, c)
	}
	return twoSided
}
