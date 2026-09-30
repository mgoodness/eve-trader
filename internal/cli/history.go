package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// historyExpiryHour and historyExpiryMinute are the daily UTC time the
// market-history route expires at (spec §6; research
// eve-market-mechanics-and-esi.md §6.2: "This route expires daily at
// 11:05").
const (
	historyExpiryHour   = 11
	historyExpiryMinute = 5
)

// HistoryFilteredUniverse runs the whole filter funnel (spec §6 "History
// funnel", §7): the book-only stage (BookFilteredUniverse), then — for
// those survivors only — one market-history call per type, cached until
// the next daily 11:05 UTC refresh (cache.UntilDailyUTC), and the three
// history-dependent filters (min history, min liquidity, price band) in
// order. No history is ever requested for a candidate the book-only stage
// already excluded, and a book-only exclusion is never overwritten with a
// second, history-stage reason.
func HistoryFilteredUniverse(ctx context.Context, cfg Config) ([]engine.Recommendation, []engine.Excluded, PilotFacts, []string, error) {
	recs, excluded, facts, warnings, err := BookFilteredUniverse(ctx, cfg)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, nil, PilotFacts{}, nil, err
	}
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	candidates := make([]engine.CandidateHistory, 0, len(recs))
	for _, rec := range recs {
		history, err := cachedHistory(ctx, cfg, store, client, rec.TypeID, time.Now())
		if err != nil {
			return nil, nil, PilotFacts{}, nil, fmt.Errorf("fetching history for type %d: %w", rec.TypeID, err)
		}
		candidates = append(candidates, engine.CandidateHistory{Recommendation: rec, History: history})
	}

	thresholds := engine.FilterThresholds{
		GrossMarginCeiling: cfg.Values.Filters.GrossMarginCeiling,
		ThinBookMinOrders:  cfg.Values.Filters.ThinBookMinOrders,
		ThinBookBandPct:    cfg.Values.Filters.ThinBookBandPct,
		MinHistoryDays:     cfg.Values.Filters.MinHistoryDays,
		MinLiquidityADV:    cfg.Values.Filters.MinLiquidityADV,
		PriceBandLow:       cfg.Values.Filters.PriceBandLow,
		PriceBandHigh:      cfg.Values.Filters.PriceBandHigh,
	}

	historyRecs, historyExcluded := engine.FilterByHistory(candidates, thresholds)
	excluded = append(excluded, historyExcluded...)

	return historyRecs, excluded, facts, warnings, nil
}

// cachedHistory returns typeID's 30-day market history from cache if fresh,
// otherwise conditionally refetches it (If-None-Match) and caches the
// result until the next daily 11:05 UTC refresh, relative to now.
func cachedHistory(ctx context.Context, cfg Config, store *cache.Store, client *esi.Client, typeID int32, now time.Time) ([]engine.HistoryRecord, error) {
	key := historyKey(cfg.RegionID, typeID)

	body, etag, fresh, err := store.GetWithETag(key)
	if err != nil {
		return nil, err
	}
	if fresh {
		return esi.DecodeHistory(body)
	}

	result, err := client.FetchHistory(ctx, cfg.RegionID, typeID, etag)
	if err != nil {
		return nil, err
	}

	respBody, respETag := result.Body, result.ETag
	if result.NotModified {
		respBody, respETag = body, etag
	}

	ttl := cache.UntilDailyUTC(now, historyExpiryHour, historyExpiryMinute)
	if err := store.SetWithETag(key, respBody, respETag, ttl); err != nil {
		return nil, err
	}

	return esi.DecodeHistory(respBody)
}

func historyKey(regionID, typeID int32) string {
	return fmt.Sprintf("history:%d:%d", regionID, typeID)
}
