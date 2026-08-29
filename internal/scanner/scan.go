package scanner

import (
	"context"
	"fmt"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/storage"
)

const (
	// orderCacheTTL matches the market order-book's own ESI cache window
	// (the Scan Cycle, per CONTEXT.md).
	orderCacheTTL = 5 * time.Minute

	// volumeCacheTTL and feeCacheTTL are both 24h: market-history only
	// refreshes daily, and Fees are cached 24h per ADR-0001.
	volumeCacheTTL = 24 * time.Hour
	feeCacheTTL    = 24 * time.Hour
)

// Scan computes every Opportunity for h with two-sided market presence,
// from cached data refreshed as needed: order-book prices at most every
// 5 minutes, and market-history volume plus fee inputs (skills,
// standings) at most every 24 hours. Ranking and filtering (top N,
// minimum volume/margin) is a separate step -- see Rank.
func Scan(ctx context.Context, client esi.Client, store *storage.Store, h hub.Hub, characterID int32, now time.Time) ([]Opportunity, error) {
	if err := refreshStaleCache(ctx, client, store, h, now); err != nil {
		return nil, err
	}

	brokerFeePct, salesTaxPct, err := feeRates(ctx, client, store, characterID, h, now)
	if err != nil {
		return nil, fmt.Errorf("compute fee rates: %w", err)
	}

	cached, err := store.LoadOpportunityCache(ctx, h.Name)
	if err != nil {
		return nil, fmt.Errorf("load opportunity cache: %w", err)
	}

	opportunities := make([]Opportunity, 0, len(cached))
	for typeID, entry := range cached {
		if entry.BestBuy == nil || entry.BestSell == nil {
			continue // no two-sided market at this Hub for this item
		}
		var avgVolume float64
		if entry.AvgVolume != nil {
			avgVolume = *entry.AvgVolume
		}
		opportunities = append(opportunities, Opportunity{
			TypeID:         typeID,
			Hub:            h,
			BestBuy:        *entry.BestBuy,
			BestSell:       *entry.BestSell,
			Margin:         Margin(*entry.BestBuy, *entry.BestSell, brokerFeePct, salesTaxPct),
			AvgDailyVolume: avgVolume,
		})
	}
	return opportunities, nil
}

// refreshStaleCache re-fetches and re-caches h's order-book prices
// and/or market-history volume, whichever have gone stale.
//
// Unlike internal/tracker's throttling (which needed a mutex to prevent
// concurrent evaluations from double-firing a Discord notification),
// concurrent Scans of the same Hub aren't guarded here: worst case is a
// harmless duplicate ESI fetch plus a redundant, idempotent cache write
// -- there's no side effect that a race could double-fire. Don't add a
// mutex here to "match" tracker's without a concrete bug driving it.
func refreshStaleCache(ctx context.Context, client esi.Client, store *storage.Store, h hub.Hub, now time.Time) error {
	meta, hasMeta, err := store.LoadScanCacheMeta(ctx, h.Name)
	if err != nil {
		return fmt.Errorf("load scan cache meta: %w", err)
	}
	meta.Hub = h.Name

	needOrders := !hasMeta || now.Sub(meta.OrdersFetchedAt) >= orderCacheTTL
	needVolume := !hasMeta || now.Sub(meta.VolumeFetchedAt) >= volumeCacheTTL

	if needOrders {
		prices, err := FetchHubPrices(ctx, client, h, ItemUniverse)
		if err != nil {
			return fmt.Errorf("fetch hub prices: %w", err)
		}

		toSave := make(map[int32]storage.OpportunityPrice, len(prices))
		for typeID, p := range prices {
			var sp storage.OpportunityPrice
			if p.HasBestBuy {
				sp.BestBuy = &p.BestBuy
			}
			if p.HasBestSell {
				sp.BestSell = &p.BestSell
			}
			toSave[typeID] = sp
		}
		if err := store.SaveOpportunityPrices(ctx, h.Name, toSave); err != nil {
			return fmt.Errorf("save opportunity prices: %w", err)
		}
		meta.OrdersFetchedAt = now
	}

	if needVolume {
		if err := refreshVolumes(ctx, client, store, h, ItemUniverse); err != nil {
			return err
		}
		meta.VolumeFetchedAt = now
	} else if err := backfillMissingVolumes(ctx, client, store, h); err != nil {
		return err
	}

	if needOrders || needVolume {
		if err := store.SaveScanCacheMeta(ctx, meta); err != nil {
			return fmt.Errorf("save scan cache meta: %w", err)
		}
	}
	return nil
}

// backfillMissingVolumes fetches and caches volume for any ItemUniverse
// item at h that has never had its volume fetched, independent of
// whether h's volume cache is otherwise fresh. Without this, an item
// added to ItemUniverse mid-cycle (see ADR-0007: expected to grow over
// time) would show a misleading 0 volume -- its cache row's AvgVolume
// is SQL NULL, which Scan treats identically to a real zero -- until
// h's next full 24h refresh (see #55). Deliberately doesn't touch
// meta.VolumeFetchedAt: this is a targeted backfill, not a hub-wide
// refresh, so already-cached items' 24h window must stay untouched.
func backfillMissingVolumes(ctx context.Context, client esi.Client, store *storage.Store, h hub.Hub) error {
	missing, err := missingVolumeItems(ctx, store, h.Name, ItemUniverse)
	if err != nil {
		return fmt.Errorf("find items missing cached volume: %w", err)
	}
	if len(missing) == 0 {
		return nil
	}
	return refreshVolumes(ctx, client, store, h, missing)
}

// refreshVolumes fetches and caches each item in items' market-history-
// derived Volume Window at h.
func refreshVolumes(ctx context.Context, client esi.Client, store *storage.Store, h hub.Hub, items []int32) error {
	volumes := make(map[int32]float64, len(items))
	for _, typeID := range items {
		history, err := client.MarketHistory(ctx, h.RegionID, typeID)
		if err != nil {
			return fmt.Errorf("fetch market history for type %d: %w", typeID, err)
		}
		volumes[typeID] = VolumeWindow(history)
	}
	if err := store.SaveOpportunityVolumes(ctx, h.Name, volumes); err != nil {
		return fmt.Errorf("save opportunity volumes: %w", err)
	}
	return nil
}

// missingVolumeItems returns the subset of items that have no cached
// average daily volume at hubName yet -- no cache row at all, or a row
// whose AvgVolume hasn't been populated (see #55).
func missingVolumeItems(ctx context.Context, store *storage.Store, hubName string, items []int32) ([]int32, error) {
	cached, err := store.LoadOpportunityCache(ctx, hubName)
	if err != nil {
		return nil, fmt.Errorf("load opportunity cache: %w", err)
	}

	var missing []int32
	for _, typeID := range items {
		if entry, ok := cached[typeID]; !ok || entry.AvgVolume == nil {
			missing = append(missing, typeID)
		}
	}
	return missing, nil
}

// feeRates returns the character's current broker-fee and sales-tax
// percentages at h, refreshing cached skills/standings from client if
// either has gone stale (>24h, or never fetched).
func feeRates(ctx context.Context, client esi.Client, store *storage.Store, characterID int32, h hub.Hub, now time.Time) (brokerFeePct, salesTaxPct float64, err error) {
	levels, skillsFetchedAt, hasSkills, err := store.LoadCharacterSkills(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load character skills: %w", err)
	}
	standings, standingsFetchedAt, hasStandings, err := store.LoadCharacterStandings(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load character standings: %w", err)
	}

	stale := !hasSkills || !hasStandings ||
		now.Sub(skillsFetchedAt) >= feeCacheTTL || now.Sub(standingsFetchedAt) >= feeCacheTTL
	if stale {
		skills, err := client.CharacterSkills(ctx, characterID)
		if err != nil {
			return 0, 0, fmt.Errorf("fetch character skills: %w", err)
		}
		rawStandings, err := client.CharacterStandings(ctx, characterID)
		if err != nil {
			return 0, 0, fmt.Errorf("fetch character standings: %w", err)
		}

		levels = make(map[int32]int32, len(skills.Skills))
		for _, s := range skills.Skills {
			levels[s.SkillID] = s.ActiveSkillLevel
		}
		standings = make(map[int32]float64, len(rawStandings))
		for _, s := range rawStandings {
			standings[s.FromID] = s.Standing
		}

		if err := store.SaveCharacterSkills(ctx, levels, now); err != nil {
			return 0, 0, fmt.Errorf("save character skills: %w", err)
		}
		if err := store.SaveCharacterStandings(ctx, standings, now); err != nil {
			return 0, 0, fmt.Errorf("save character standings: %w", err)
		}
	}

	skills := esi.Skills{Skills: make([]esi.Skill, 0, len(levels))}
	for id, lvl := range levels {
		skills.Skills = append(skills.Skills, esi.Skill{SkillID: id, ActiveSkillLevel: lvl})
	}
	esiStandings := make([]esi.Standing, 0, len(standings))
	for id, st := range standings {
		esiStandings = append(esiStandings, esi.Standing{FromID: id, Standing: st})
	}

	return BrokerFeePct(skills, esiStandings, h), SalesTaxPct(skills), nil
}
