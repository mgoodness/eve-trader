package engine

import "strconv"

// npcStationCeiling is the location-ID boundary below which a location is an
// NPC station (6-10 digits) and above which it is an Upwell structure
// (13-digit IDs, e.g. 1031084757448). See notes on ticket #10.
const npcStationCeiling = 1_000_000_000

// IsNPCStation reports whether locationID is an NPC station rather than an
// Upwell structure. Exported so callers outside this package (the cli
// adapter, deciding which systems are worth a jump-distance route lookup)
// can apply the same NPC-only scope without duplicating the boundary.
func IsNPCStation(locationID int64) bool {
	return locationID < npcStationCeiling
}

// EffectiveSellBook returns the sell orders located at the trade station
// (spec §3, §6): sellers elsewhere do not compete for the station's buyers.
func EffectiveSellBook(orders []Order, tradeStationID int64) []Order {
	var book []Order
	for _, o := range orders {
		if !o.IsBuyOrder && o.LocationID == tradeStationID {
			book = append(book, o)
		}
	}
	return book
}

// EffectiveBuyBook returns the buy orders whose range covers the trade
// station (spec §3, §6): the competing bids, wherever they sit in the
// region. station/solarsystem/region ranges cover by direct match; a
// numeric range (a jump count, "1".. "40") covers exactly when the order's
// system's jump distance to the trade system — looked up in jumpDistances,
// keyed by system ID — is at most that count (ticket #18). A system
// missing from jumpDistances (never queried, or a failed route lookup) is
// treated as not covering: the caller is responsible for populating
// jumpDistances for every system it can resolve and warning on the ones it
// can't (spec §6). NPC-station locations only — a structure-located order
// is excluded regardless of its range.
func EffectiveBuyBook(orders []Order, tradeStationID int64, tradeSystemID int32, jumpDistances map[int32]int) []Order {
	var book []Order
	for _, o := range orders {
		if !o.IsBuyOrder || !IsNPCStation(o.LocationID) {
			continue
		}
		var covers bool
		switch o.Range {
		case "station":
			covers = o.LocationID == tradeStationID
		case "solarsystem":
			covers = o.SystemID == tradeSystemID
		case "region":
			covers = true
		default:
			if rangeJumps, err := strconv.Atoi(o.Range); err == nil {
				if distance, ok := jumpDistances[o.SystemID]; ok {
					covers = distance <= rangeJumps
				}
			}
		}
		if covers {
			book = append(book, o)
		}
	}
	return book
}
