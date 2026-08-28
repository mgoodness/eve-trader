package scanner

import (
	"context"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

// maxMarketPages bounds FetchHubPrices's pagination loop -- a sane upper
// limit well above the largest observed regional order-book page count
// (~411 pages for The Forge/Jita, per
// docs/research/esi-market-endpoints.md), so a malformed response can't
// spin forever.
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

// FetchHubPrices sweeps h's full regional order book (there is no
// per-station ESI endpoint, so the region-wide result must be filtered
// client-side, per docs/research/esi-market-endpoints.md) and returns
// the best buy/sell price at h's station for each item in itemUniverse
// that has orders there.
func FetchHubPrices(ctx context.Context, client esi.Client, h hub.Hub, itemUniverse []int32) (map[int32]HubPrice, error) {
	wanted := make(map[int32]bool, len(itemUniverse))
	for _, id := range itemUniverse {
		wanted[id] = true
	}

	prices := make(map[int32]HubPrice)

	for page := int32(1); page <= maxMarketPages; page++ {
		entries, err := client.MarketOrders(ctx, h.RegionID, esi.OrderTypeAll, page)
		if err != nil {
			return nil, fmt.Errorf("fetch market orders (page %d): %w", page, err)
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			if e.LocationID != h.StationID || !wanted[e.TypeID] {
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

	return prices, nil
}
