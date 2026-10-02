package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// wireCharacterOrder mirrors one record of GET /characters/{id}/orders/ and
// /orders/history/ (docs/research/esi-assets-and-orders.md §2.1). State is
// populated only by the history route.
type wireCharacterOrder struct {
	OrderID       int64     `json:"order_id"`
	TypeID        int32     `json:"type_id"`
	RegionID      int32     `json:"region_id"`
	LocationID    int64     `json:"location_id"`
	Range         string    `json:"range"`
	IsBuyOrder    bool      `json:"is_buy_order"`
	IsCorporation bool      `json:"is_corporation"`
	Price         float64   `json:"price"`
	VolumeTotal   int64     `json:"volume_total"`
	VolumeRemain  int64     `json:"volume_remain"`
	MinVolume     int64     `json:"min_volume"`
	Duration      int       `json:"duration"`
	Issued        time.Time `json:"issued"`
	Escrow        float64   `json:"escrow"`
	State         string    `json:"state"`
}

func (o wireCharacterOrder) toEngineCharacterOrder() engine.CharacterOrder {
	return engine.CharacterOrder{
		OrderID:       o.OrderID,
		TypeID:        o.TypeID,
		RegionID:      o.RegionID,
		LocationID:    o.LocationID,
		Range:         o.Range,
		IsBuyOrder:    o.IsBuyOrder,
		IsCorporation: o.IsCorporation,
		Price:         o.Price,
		VolumeTotal:   o.VolumeTotal,
		VolumeRemain:  o.VolumeRemain,
		MinVolume:     o.MinVolume,
		Duration:      o.Duration,
		Issued:        o.Issued,
		Escrow:        o.Escrow,
		State:         o.State,
	}
}

// wireAsset mirrors one record of GET /characters/{id}/assets/ (research
// esi-assets-and-orders.md §1.1).
type wireAsset struct {
	ItemID       int64  `json:"item_id"`
	TypeID       int32  `json:"type_id"`
	Quantity     int64  `json:"quantity"`
	LocationID   int64  `json:"location_id"`
	LocationType string `json:"location_type"`
	LocationFlag string `json:"location_flag"`
	IsSingleton  bool   `json:"is_singleton"`
}

func (a wireAsset) toEngineAsset() engine.Asset {
	return engine.Asset{
		ItemID:       a.ItemID,
		TypeID:       a.TypeID,
		Quantity:     a.Quantity,
		LocationID:   a.LocationID,
		LocationType: a.LocationType,
		LocationFlag: a.LocationFlag,
		IsSingleton:  a.IsSingleton,
	}
}

// CharacterOrders fetches the character's open orders (spec §7 step 1;
// research esi-assets-and-orders.md §2.1). The route has no pagination: one
// response carries the character's whole open-order list (research §2.4).
func (c *Client) CharacterOrders(ctx context.Context, characterID int32, accessToken string) ([]engine.CharacterOrder, error) {
	reqURL := fmt.Sprintf("%s/characters/%d/orders/", c.baseURL, characterID)
	body, _, err := c.getAuthenticatedWithHeaders(ctx, reqURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetching character orders: %w", err)
	}
	return decodeCharacterOrders(body)
}

// CharacterOrderHistory fetches the character's cancelled/expired order
// history, paging through X-Pages (spec §7 step 2; research
// esi-assets-and-orders.md §2.4). The route has no `filled` state — only
// cancelled/expired (research §2.3.1).
func (c *Client) CharacterOrderHistory(ctx context.Context, characterID int32, accessToken string) ([]engine.CharacterOrder, error) {
	var orders []engine.CharacterOrder
	err := c.fetchAuthenticatedPages(ctx, accessToken, func(page int) string {
		return fmt.Sprintf("%s/characters/%d/orders/history/?page=%d", c.baseURL, characterID, page)
	}, func(body []byte) error {
		decoded, err := decodeCharacterOrders(body)
		if err != nil {
			return err
		}
		orders = append(orders, decoded...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fetching character order history: %w", err)
	}
	return orders, nil
}

// CharacterAssets fetches the character's full asset list, paging through
// X-Pages (spec §7 step 3; research esi-assets-and-orders.md §1.2). The
// route has no server-side station filter, so the caller filters
// client-side.
func (c *Client) CharacterAssets(ctx context.Context, characterID int32, accessToken string) ([]engine.Asset, error) {
	var assets []engine.Asset
	err := c.fetchAuthenticatedPages(ctx, accessToken, func(page int) string {
		return fmt.Sprintf("%s/characters/%d/assets/?page=%d", c.baseURL, characterID, page)
	}, func(body []byte) error {
		var wire []wireAsset
		if err := json.Unmarshal(body, &wire); err != nil {
			return fmt.Errorf("decoding character assets: %w", err)
		}
		for _, a := range wire {
			assets = append(assets, a.toEngineAsset())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fetching character assets: %w", err)
	}
	return assets, nil
}

// fetchAuthenticatedPages issues urlFor(page) pages until X-Pages reports
// the last one, calling onPage for each body. It is the shared paging loop
// for the character routes that paginate (assets, order history).
func (c *Client) fetchAuthenticatedPages(ctx context.Context, accessToken string, urlFor func(page int) string, onPage func(body []byte) error) error {
	page := 1
	for {
		body, header, err := c.getAuthenticatedWithHeaders(ctx, urlFor(page), accessToken)
		if err != nil {
			return err
		}
		if err := onPage(body); err != nil {
			return err
		}

		pages := 1
		if xp := header.Get("X-Pages"); xp != "" {
			if n, err := strconv.Atoi(xp); err == nil {
				pages = n
			}
		}
		if page >= pages {
			return nil
		}
		page++
	}
}

// decodeCharacterOrders decodes one response body of the character-order
// routes into engine.CharacterOrder values.
func decodeCharacterOrders(body []byte) ([]engine.CharacterOrder, error) {
	var wire []wireCharacterOrder
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decoding character orders: %w", err)
	}
	orders := make([]engine.CharacterOrder, 0, len(wire))
	for _, o := range wire {
		orders = append(orders, o.toEngineCharacterOrder())
	}
	return orders, nil
}
