package scanner

import (
	"context"
	"fmt"
	"sync"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

// maxMarketPages bounds the pagination loop for a single item type's
// order book -- comfortably above anything one item type could ever
// span, so a malformed response can't spin forever.
const maxMarketPages = 500

// maxConcurrentFetches bounds how many ItemUniverse members' order
// books FetchHubPrices fetches at once -- unbounded concurrency risks
// tripping ESI's error-rate limiting. Measured against live ESI (#57):
// a 40-item Hub refresh went from ~18s sequential to ~2s at this limit,
// since every item's fetch is independent of every other.
const maxConcurrentFetches = 10

// HubPrice is the best buy/sell price found for one item at one Hub.
// HasBestBuy/HasBestSell are false when no order exists on that side --
// distinct from a price of zero.
type HubPrice struct {
	BestBuy     float64
	HasBestBuy  bool
	BestSell    float64
	HasBestSell bool
}

// FetchHubPrices returns the best buy/sell price at h's station for each
// item in itemUniverse, querying each item's order book separately via
// ESI's type_id filter -- sweeping h's region unfiltered runs 400+ pages
// for a hub like Jita, far too slow to do synchronously per request.
// There is no per-station ESI endpoint, so results are still filtered
// client-side by LocationID to isolate h.
//
// Each item's fetch is independent of every other, so they run
// concurrently (bounded by maxConcurrentFetches) rather than one at a
// time (see #57). The first error from any item cancels the rest and is
// returned; a partial result is never returned alongside an error.
func FetchHubPrices(ctx context.Context, client esi.Client, h hub.Hub, itemUniverse []int32) (map[int32]HubPrice, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		prices   = make(map[int32]HubPrice)
		sem      = make(chan struct{}, maxConcurrentFetches)
		errOnce  sync.Once
		firstErr error
	)

dispatch:
	for _, typeID := range itemUniverse {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			// An earlier item's error already canceled ctx -- stop
			// launching new fetches rather than dispatching the rest
			// of itemUniverse against ESI for a result we'll discard.
			break dispatch
		}

		wg.Add(1)
		go func(typeID int32) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			p, found, err := fetchItemPrice(ctx, client, h, typeID)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			if !found {
				return
			}

			mu.Lock()
			prices[typeID] = p
			mu.Unlock()
		}(typeID)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return prices, nil
}

// fetchItemPrice fetches typeID's paginated order book at h and reduces
// it to the best buy/sell price. found is false when no order at all
// matched h's station for typeID, distinct from a price of zero.
func fetchItemPrice(ctx context.Context, client esi.Client, h hub.Hub, typeID int32) (p HubPrice, found bool, err error) {
	for page := int32(1); page <= maxMarketPages; page++ {
		entries, err := client.MarketOrders(ctx, h.RegionID, esi.OrderTypeAll, typeID, page)
		if err != nil {
			return HubPrice{}, false, fmt.Errorf("fetch market orders (type %d, page %d): %w", typeID, page, err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			if e.LocationID != h.StationID || e.TypeID != typeID {
				continue
			}

			found = true
			if e.IsBuyOrder {
				if !p.HasBestBuy || e.Price > p.BestBuy {
					p.BestBuy, p.HasBestBuy = e.Price, true
				}
			} else {
				if !p.HasBestSell || e.Price < p.BestSell {
					p.BestSell, p.HasBestSell = e.Price, true
				}
			}
		}
	}
	return p, found, nil
}
