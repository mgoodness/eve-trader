package engine

// npcStationCeiling is the location-ID boundary below which a location is an
// NPC station (6-10 digits) and above which it is an Upwell structure
// (13-digit IDs, e.g. 1031084757448). See notes on ticket #10.
const npcStationCeiling = 1_000_000_000

func isNPCStation(locationID int64) bool {
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
// region. Coverage is evaluated by station, solarsystem, or region match
// only; numeric jump-range coverage is deferred to ticket #18, so a numeric
// range never covers here. NPC-station locations only — a structure-located
// order is excluded regardless of its range.
func EffectiveBuyBook(orders []Order, tradeStationID int64, tradeSystemID int32) []Order {
	var book []Order
	for _, o := range orders {
		if !o.IsBuyOrder || !isNPCStation(o.LocationID) {
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
		}
		if covers {
			book = append(book, o)
		}
	}
	return book
}
