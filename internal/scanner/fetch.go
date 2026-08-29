package scanner

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

// maxMarketPages bounds the pagination loop for a single item type's
// order book -- comfortably above anything one item type could ever
// span, so a malformed response can't spin forever.
const maxMarketPages = 500

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
func FetchHubPrices(ctx context.Context, client esi.Client, h hub.Hub, itemUniverse []int32) (map[int32]HubPrice, error) {
	prices := make(map[int32]HubPrice)

	for _, typeID := range itemUniverse {
		for page := int32(1); page <= maxMarketPages; page++ {
			entries, err := client.MarketOrders(ctx, h.RegionID, esi.OrderTypeAll, typeID, page)
			if err != nil {
				return nil, fmt.Errorf("fetch market orders (type %d, page %d): %w", typeID, page, err)
			}
			if len(entries) == 0 {
				break
			}

			for _, e := range entries {
				if e.LocationID != h.StationID || e.TypeID != typeID {
					continue
				}

				p := prices[e.TypeID]
				if e.IsBuyOrder {
					if !p.HasBestBuy || e.Price > p.BestBuy {
						p.BestBuy, p.HasBestBuy = e.Price, true
					}
				} else {
					if !p.HasBestSell || e.Price < p.BestSell {
						p.BestSell, p.HasBestSell = e.Price, true
					}
				}
				prices[e.TypeID] = p
			}
		}
	}

	return prices, nil
}
