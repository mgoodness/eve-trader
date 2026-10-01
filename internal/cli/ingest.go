package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// RegionFeed fetches the whole Heimatar region-orders feed (spec §6): every
// page of GET /markets/{regionID}/orders/?order_type=all. All pages share a
// single 300s cache window (regionOrdersTTL) and are refetched together:
// the refresh decision is keyed off page 1's freshness, and once triggered,
// every page is conditionally refetched (If-None-Match) and the whole
// batch's cache entries are only committed after every page succeeds — a
// mid-refresh failure leaves the prior batch in place rather than mixing
// page generations. A 304 reuses the page's previously cached body instead
// of decoding an empty one.
//
// RegionFeed returns every order in the feed, unfiltered: NPC-station-only
// filtering and the effective sell/buy books are computed downstream by
// engine.Universe / EffectiveSellBook / EffectiveBuyBook.
func RegionFeed(ctx context.Context, cfg Config) ([]engine.Order, error) {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	_, _, page1Fresh, err := store.GetWithETag(regionFeedPageKey(cfg.RegionID, 1))
	if err != nil {
		return nil, err
	}
	if !page1Fresh {
		if err := refreshRegionFeed(ctx, cfg, store); err != nil {
			return nil, err
		}
	}

	pages, err := regionFeedPageCount(store, cfg.RegionID)
	if err != nil {
		return nil, err
	}

	var orders []engine.Order
	for page := 1; page <= pages; page++ {
		key := regionFeedPageKey(cfg.RegionID, page)
		body, _, fresh, err := store.GetWithETag(key)
		if err != nil {
			return nil, err
		}
		if !fresh {
			return nil, fmt.Errorf("region feed page %d is missing or went stale mid-read", page)
		}
		decoded, err := esi.DecodeOrders(body)
		if err != nil {
			return nil, fmt.Errorf("decoding region orders page %d: %w", page, err)
		}
		orders = append(orders, decoded...)
	}

	return orders, nil
}

// refreshRegionFeed fetches every page of the region feed, conditionally
// against whatever ETags are already cached (fresh or stale), and commits
// the whole batch's cache entries only once every page has succeeded.
func refreshRegionFeed(ctx context.Context, cfg Config, store *cache.Store) error {
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	previousPages, err := regionFeedPageCount(store, cfg.RegionID)
	if err != nil {
		return err
	}

	type fetchedPage struct {
		body []byte
		etag string
	}

	pages := previousPages
	fetched := make([]fetchedPage, 0, pages)
	for page := 1; page <= pages; page++ {
		key := regionFeedPageKey(cfg.RegionID, page)
		_, etag, _, err := store.GetWithETag(key)
		if err != nil {
			return err
		}

		result, err := client.FetchRegionOrdersPage(ctx, cfg.RegionID, "all", 0, page, etag)
		if err != nil {
			return fmt.Errorf("fetching region orders page %d: %w", page, err)
		}

		body, resultETag := result.Body, result.ETag
		if result.NotModified {
			cachedBody, cachedETag, _, err := store.GetWithETag(key)
			if err != nil {
				return err
			}
			body, resultETag = cachedBody, cachedETag
		} else if page == 1 && result.Pages > 0 {
			pages = result.Pages
		}

		fetched = append(fetched, fetchedPage{body: body, etag: resultETag})
	}

	for i, fp := range fetched {
		page := i + 1
		if err := store.SetWithETag(regionFeedPageKey(cfg.RegionID, page), fp.body, fp.etag, regionOrdersTTL); err != nil {
			return err
		}
	}
	return store.SetWithETag(regionFeedPageCountKey(cfg.RegionID), []byte(strconv.Itoa(pages)), "", regionOrdersTTL)
}

func regionFeedPageKey(regionID int32, page int) string {
	return fmt.Sprintf("region-orders:%d:page:%d", regionID, page)
}

func regionFeedPageCountKey(regionID int32) string {
	return fmt.Sprintf("region-orders:%d:pages", regionID)
}

// regionFeedPageCount returns the cached page count for regionID, or 1 if
// none is cached yet (a fresh install: page 1's response will reveal the
// true count on the first refresh).
func regionFeedPageCount(store *cache.Store, regionID int32) (int, error) {
	raw, _, _, err := store.GetWithETag(regionFeedPageCountKey(regionID))
	if err != nil {
		return 0, err
	}
	if len(raw) == 0 {
		return 1, nil
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 1 {
		return 1, nil
	}
	return n, nil
}
