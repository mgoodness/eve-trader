// Package hub defines the two Hubs this tool trades at (see CONTEXT.md):
// Jita and Rens. Every ESI identifier tied to a Hub -- its trade-hub
// station, region, and (for broker-fee computation) the NPC corporation
// and faction that own that station -- lives here once, rather than
// duplicated as raw IDs across the packages that need them.
package hub

// Hub identifies one of the two trading locations this tool scans
// independently. See docs/research/esi-market-endpoints.md for how these
// IDs were confirmed against live ESI universe endpoints.
type Hub struct {
	Name      string
	RegionID  int32
	StationID int64

	// OwnerCorpID and OwnerFactionID are the NPC corporation and faction
	// that own the Hub's trade-hub station, needed to look up the
	// character's standings with them for broker-fee computation (see
	// docs/adr/0006-fee-computation-formulas.md).
	OwnerCorpID    int32
	OwnerFactionID int32
}

var (
	// Jita is The Forge region's trade hub, at Jita IV - Moon 4 -
	// Caldari Navy Assembly Plant (owned by the Caldari Navy, part of
	// the Caldari State).
	Jita = Hub{
		Name:           "Jita",
		RegionID:       10000002,
		StationID:      60003760,
		OwnerCorpID:    1000035,
		OwnerFactionID: 500001,
	}

	// Rens is Heimatar region's trade hub, at Rens VI - Moon 8 - Brutor
	// Tribe Treasury (owned by the Brutor Tribe, part of the Minmatar
	// Republic).
	Rens = Hub{
		Name:           "Rens",
		RegionID:       10000030,
		StationID:      60004588,
		OwnerCorpID:    1000049,
		OwnerFactionID: 500002,
	}

	// All is every Hub this tool trades at.
	All = []Hub{Jita, Rens}
)

// ByStationID finds the Hub whose trade-hub station matches stationID.
func ByStationID(stationID int64) (Hub, bool) {
	for _, h := range All {
		if h.StationID == stationID {
			return h, true
		}
	}
	return Hub{}, false
}

// ByName finds the Hub with the given Name (case-sensitive, matching the
// exact names above).
func ByName(name string) (Hub, bool) {
	for _, h := range All {
		if h.Name == name {
			return h, true
		}
	}
	return Hub{}, false
}
