// Package esi is the ESI HTTP client: region orders today, with history,
// route, skills, and standings added by later tickets. It owns
// X-Compatibility-Date, User-Agent, and pagination; disk caching and
// rate-limit backoff are layered on by the CLI adapter and later tickets.
package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mgoodness/eve-trader/internal/engine"
)

const (
	defaultBaseURL    = "https://esi.evetech.net/latest"
	defaultSSOBaseURL = "https://login.eveonline.com"
)

// ClientOptions configures a Client. BaseURL, SSOBaseURL, CompatDate, and
// UserAgent all have sane defaults if left zero, except UserAgent: ESI's
// best-practices docs require a contactable User-Agent, so callers should
// always set one.
type ClientOptions struct {
	BaseURL    string
	SSOBaseURL string
	UserAgent  string
	CompatDate string
	HTTPClient *http.Client
}

// Client is an ESI HTTP client.
type Client struct {
	baseURL    string
	ssoBaseURL string
	userAgent  string
	compatDate string
	httpClient *http.Client
}

// NewClient returns a Client configured with opts.
func NewClient(opts ClientOptions) *Client {
	c := &Client{
		baseURL:    opts.BaseURL,
		ssoBaseURL: opts.SSOBaseURL,
		userAgent:  opts.UserAgent,
		compatDate: opts.CompatDate,
		httpClient: opts.HTTPClient,
	}
	if c.baseURL == "" {
		c.baseURL = defaultBaseURL
	}
	if c.ssoBaseURL == "" {
		c.ssoBaseURL = defaultSSOBaseURL
	}
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	return c
}

// regionOrder mirrors one record of the region-orders wire format
// (docs/research/eve-market-mechanics-and-esi.md §6.1).
type regionOrder struct {
	OrderID      int64   `json:"order_id"`
	TypeID       int32   `json:"type_id"`
	LocationID   int64   `json:"location_id"`
	SystemID     int32   `json:"system_id"`
	VolumeTotal  int64   `json:"volume_total"`
	VolumeRemain int64   `json:"volume_remain"`
	MinVolume    int64   `json:"min_volume"`
	Price        float64 `json:"price"`
	IsBuyOrder   bool    `json:"is_buy_order"`
	Range        string  `json:"range"`
}

func (o regionOrder) toEngineOrder() engine.Order {
	return engine.Order{
		OrderID:      o.OrderID,
		TypeID:       o.TypeID,
		LocationID:   o.LocationID,
		SystemID:     o.SystemID,
		Price:        o.Price,
		VolumeRemain: o.VolumeRemain,
		MinVolume:    o.MinVolume,
		IsBuyOrder:   o.IsBuyOrder,
		Range:        o.Range,
	}
}

// RegionOrders fetches every page of GET /markets/{regionID}/orders/ for the
// given order type ("all", "buy", or "sell"), optionally filtered to a
// single typeID (pass 0 to fetch every type in the region — a ~71-page pull
// for Heimatar, deferred to a later ticket's full ingest).
func (c *Client) RegionOrders(ctx context.Context, regionID int32, orderType string, typeID int32) ([]engine.Order, error) {
	var orders []engine.Order

	page := 1
	for {
		body, pages, err := c.getRegionOrdersPage(ctx, regionID, orderType, typeID, page)
		if err != nil {
			return nil, err
		}

		var wire []regionOrder
		if err := json.Unmarshal(body, &wire); err != nil {
			return nil, fmt.Errorf("decoding region orders page %d: %w", page, err)
		}
		for _, o := range wire {
			orders = append(orders, o.toEngineOrder())
		}

		if page >= pages {
			break
		}
		page++
	}

	return orders, nil
}

func (c *Client) getRegionOrdersPage(ctx context.Context, regionID int32, orderType string, typeID int32, page int) (body []byte, pages int, err error) {
	q := url.Values{}
	q.Set("order_type", orderType)
	q.Set("page", strconv.Itoa(page))
	if typeID != 0 {
		q.Set("type_id", strconv.Itoa(int(typeID)))
	}

	reqURL := fmt.Sprintf("%s/markets/%d/orders/?%s", c.baseURL, regionID, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.compatDate != "" {
		req.Header.Set("X-Compatibility-Date", c.compatDate)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("GET %s: unexpected status %s", reqURL, resp.Status)
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	pages = 1
	if xp := resp.Header.Get("X-Pages"); xp != "" {
		if n, err := strconv.Atoi(xp); err == nil {
			pages = n
		}
	}

	return body, pages, nil
}
