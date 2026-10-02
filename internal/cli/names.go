package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// typeNamesTTL is the disk-cache window for a resolved type name. Names are
// effectively immutable, so they are cached for 30 days and only re-fetched
// for a type never seen before (spec §6's cache is per route; /universe/names/
// is not part of the region feed).
const typeNamesTTL = 30 * 24 * time.Hour

// namesBatchSize is the maximum number of type ids per POST
// /universe/names/ request. ESI documents a 1,000-id cap.
const namesBatchSize = 1000

// populateNames resolves every type id in the output contract to a display
// name (spec §11 `name`) via a batched, disk-cached POST /universe/names/
// lookup, writing each Name in place. The lookup is best-effort: a failed
// batch becomes a warning and leaves its ids' Names empty, so the table's
// "type <id>" fallback is reserved for a genuinely unresolved id rather than
// failing the run.
func populateNames(ctx context.Context, cfg Config, funded, unfunded []engine.BuyRecommendation, excluded []engine.Excluded, sells []engine.SellRecommendation, pending []engine.Pending) ([]string, error) {
	ids := collectTypeIDs(funded, unfunded, excluded, sells, pending)
	if len(ids) == 0 {
		return nil, nil
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, err
	}
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	names, warnings := cachedTypeNames(ctx, store, client, ids)
	applyTypeNames(names, funded, unfunded, excluded, sells, pending)
	return warnings, nil
}

// collectTypeIDs returns the distinct type ids the output contract names:
// every funded and unfunded recommendation, every excluded reason, and
// every sell recommendation and pending entry.
func collectTypeIDs(funded, unfunded []engine.BuyRecommendation, excluded []engine.Excluded, sells []engine.SellRecommendation, pending []engine.Pending) []int32 {
	size := len(funded) + len(unfunded) + len(excluded) + len(sells) + len(pending)
	seen := make(map[int32]struct{}, size)
	ids := make([]int32, 0, size)
	add := func(id int32) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, rec := range funded {
		add(rec.TypeID)
	}
	for _, rec := range unfunded {
		add(rec.TypeID)
	}
	for _, e := range excluded {
		add(e.TypeID)
	}
	for _, rec := range sells {
		add(rec.TypeID)
	}
	for _, p := range pending {
		add(p.TypeID)
	}
	return ids
}

// cachedTypeNames resolves ids from the disk cache, fetching only the
// missing ones in batches through client.Names and caching each resolved
// name individually. Unresolved ids are simply absent from the result.
func cachedTypeNames(ctx context.Context, store *cache.Store, client *esi.Client, ids []int32) (map[int32]string, []string) {
	names := make(map[int32]string, len(ids))
	var missing []int32
	for _, id := range ids {
		body, fresh, err := store.Get(typeNameKey(id))
		if err == nil && fresh && len(body) > 0 {
			names[id] = string(body)
			continue
		}
		missing = append(missing, id)
	}

	var warnings []string
	for start := 0; start < len(missing); start += namesBatchSize {
		end := start + namesBatchSize
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]

		resolved, err := client.Names(ctx, batch)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("resolving %d type names: %v", len(batch), err))
			continue
		}
		for _, id := range batch {
			name, ok := resolved[id]
			if !ok || name == "" {
				continue
			}
			names[id] = name
			_ = store.Set(typeNameKey(id), []byte(name), typeNamesTTL)
		}
	}
	return names, warnings
}

func applyTypeNames(names map[int32]string, funded, unfunded []engine.BuyRecommendation, excluded []engine.Excluded, sells []engine.SellRecommendation, pending []engine.Pending) {
	for i := range funded {
		if name, ok := names[funded[i].TypeID]; ok {
			funded[i].Name = name
		}
	}
	for i := range unfunded {
		if name, ok := names[unfunded[i].TypeID]; ok {
			unfunded[i].Name = name
		}
	}
	for i := range excluded {
		if name, ok := names[excluded[i].TypeID]; ok {
			excluded[i].Name = name
		}
	}
	for i := range sells {
		if name, ok := names[sells[i].TypeID]; ok {
			sells[i].Name = name
		}
	}
	for i := range pending {
		if name, ok := names[pending[i].TypeID]; ok {
			pending[i].Name = name
		}
	}
}

func typeNameKey(typeID int32) string {
	return fmt.Sprintf("type-name:%d", typeID)
}
