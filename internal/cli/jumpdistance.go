package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// jumpsTTL is the ESI cache window for jump-distance route lookups (spec
// §6).
const jumpsTTL = 86400 * time.Second

// JumpDistances resolves the jump distance from cfg.TradeSystemID to every
// solar system referenced by a numeric-range buy order at an NPC station in
// orders (spec §6, ticket #18): one route lookup per distinct system not
// yet cached, cached for 86,400 s afterward. A failed lookup omits that
// system from the returned map — engine.EffectiveBuyBook then treats any
// numeric-range order sitting there as not covering the trade station — and
// appends a warning describing the failure to the returned slice. The
// trade system itself never needs a lookup: its distance is always 0.
func JumpDistances(ctx context.Context, cfg Config, store *cache.Store, orders []engine.Order) (map[int32]int, []string) {
	distances := map[int32]int{cfg.TradeSystemID: 0}
	var warnings []string

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	seen := map[int32]bool{cfg.TradeSystemID: true}
	for _, o := range orders {
		if !o.IsBuyOrder || !engine.IsNPCStation(o.LocationID) || seen[o.SystemID] {
			continue
		}
		if _, err := strconv.Atoi(o.Range); err != nil {
			continue
		}
		seen[o.SystemID] = true

		distance, err := cachedJumpDistance(ctx, cfg, store, client, o.SystemID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("route lookup from system %d to %d failed, excluding its numeric-range orders: %v", o.SystemID, cfg.TradeSystemID, err))
			continue
		}
		distances[o.SystemID] = distance
	}

	return distances, warnings
}

func cachedJumpDistance(ctx context.Context, cfg Config, store *cache.Store, client *esi.Client, systemID int32) (int, error) {
	key := jumpDistanceKey(systemID, cfg.TradeSystemID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		if n, err := strconv.Atoi(string(body)); err == nil {
			return n, nil
		}
	}

	route, err := client.Route(ctx, systemID, cfg.TradeSystemID)
	if err != nil {
		return 0, err
	}
	distance := len(route) - 1

	_ = store.Set(key, []byte(strconv.Itoa(distance)), jumpsTTL)
	return distance, nil
}

func jumpDistanceKey(systemID, tradeSystemID int32) string {
	return fmt.Sprintf("route:%d:%d", systemID, tradeSystemID)
}
