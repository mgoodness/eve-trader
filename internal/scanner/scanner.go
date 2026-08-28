// Package scanner implements the Opportunity Scanner: ranked
// Opportunities computed independently per Hub, with live fee-adjusted
// Margin and a 7-day trailing Volume Window.
package scanner

import (
	"sort"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

// volumeWindowDays is the trailing period Volume Window averages over
// (see CONTEXT.md's Volume Window definition).
const volumeWindowDays = 7

// Opportunity is one ranked entry: a single item at a single Hub, with
// its Margin and other ranking fields, having passed the configured
// filters (see CONTEXT.md's Opportunity definition).
type Opportunity struct {
	TypeID         int32
	Hub            hub.Hub
	BestBuy        float64
	BestSell       float64
	Margin         float64
	AvgDailyVolume float64
}

// Filter bounds which Opportunities Rank keeps.
type Filter struct {
	MinVolume float64
	MinMargin float64
}

// VolumeWindow averages the most recent volumeWindowDays days of volume
// from history (ESI's market-history, which returns ~400 days per call,
// so no backfill is needed to compute this immediately). If history has
// fewer days than the window, it averages whatever's available.
func VolumeWindow(history []esi.MarketHistoryEntry) float64 {
	if len(history) == 0 {
		return 0
	}

	sorted := make([]esi.MarketHistoryEntry, len(history))
	copy(sorted, history)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })

	n := min(volumeWindowDays, len(sorted))
	var sum float64
	for _, e := range sorted[:n] {
		sum += float64(e.Volume)
	}
	return sum / float64(n)
}

// Rank filters opportunities to those meeting f, sorts by descending
// Margin, and truncates to at most limit results. The input slice is not
// modified.
func Rank(opportunities []Opportunity, f Filter, limit int) []Opportunity {
	kept := make([]Opportunity, 0, len(opportunities))
	for _, o := range opportunities {
		if o.AvgDailyVolume < f.MinVolume || o.Margin < f.MinMargin {
			continue
		}
		kept = append(kept, o)
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].Margin > kept[j].Margin })

	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}
