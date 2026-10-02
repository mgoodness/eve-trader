// Package esi is the ESI HTTP client: region orders today, with history,
// route, skills, and standings added by later tickets. It owns
// X-Compatibility-Date, User-Agent, pagination, conditional (ETag)
// requests, and 429/420 backoff; disk caching itself (deciding what to
// store and for how long) is layered on by the CLI adapter.
package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mgoodness/eve-trader/internal/engine"
)

const (
	defaultBaseURL    = "https://esi.evetech.net/latest"
	defaultSSOBaseURL = "https://login.eveonline.com"
)

// maxBackoffAttempts bounds how many times FetchRegionOrdersPage retries a
// 429/420 response before giving up (spec "caching & politeness": back off
// on 429/420, not retry forever).
const maxBackoffAttempts = 5

// defaultBackoff is the sleep used to back off a 420 (error-limit)
// response, which carries no Retry-After header, and any 429 that omits
// one.
const defaultBackoff = time.Second

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
	// Sleep backs off between retries of a 429/420 response. Defaults to
	// time.Sleep; tests override it to avoid real waits.
	Sleep func(time.Duration)
}

// Client is an ESI HTTP client.
type Client struct {
	baseURL    string
	ssoBaseURL string
	userAgent  string
	compatDate string
	httpClient *http.Client
	sleep      func(time.Duration)
}

// NewClient returns a Client configured with opts.
func NewClient(opts ClientOptions) *Client {
	c := &Client{
		baseURL:    opts.BaseURL,
		ssoBaseURL: opts.SSOBaseURL,
		userAgent:  opts.UserAgent,
		compatDate: opts.CompatDate,
		httpClient: opts.HTTPClient,
		sleep:      opts.Sleep,
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
	if c.sleep == nil {
		c.sleep = time.Sleep
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

		decoded, err := DecodeOrders(body)
		if err != nil {
			return nil, fmt.Errorf("decoding region orders page %d: %w", page, err)
		}
		orders = append(orders, decoded...)

		if page >= pages {
			break
		}
		page++
	}

	return orders, nil
}

// DecodeOrders decodes one page body of GET /markets/{regionID}/orders/
// into engine.Order values. Exported so a caller that fetches and caches
// pages itself (the whole-feed ingest, RegionFeed) can decode a
// server-fetched or cache-reused body the same way RegionOrders does.
func DecodeOrders(body []byte) ([]engine.Order, error) {
	var wire []regionOrder
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	orders := make([]engine.Order, 0, len(wire))
	for _, o := range wire {
		orders = append(orders, o.toEngineOrder())
	}
	return orders, nil
}

func (c *Client) getRegionOrdersPage(ctx context.Context, regionID int32, orderType string, typeID int32, page int) (body []byte, pages int, err error) {
	result, err := c.FetchRegionOrdersPage(ctx, regionID, orderType, typeID, page, "")
	if err != nil {
		return nil, 0, err
	}
	return result.Body, result.Pages, nil
}

// PageResult is one page of GET /markets/{regionID}/orders/: its body,
// ETag, and the feed's total page count. NotModified reports a 304
// (ifNoneMatch matched the server's current ETag); Body and Pages are then
// empty/zero, and the caller should reuse its previously cached page.
type PageResult struct {
	Body        []byte
	ETag        string
	Pages       int
	NotModified bool
}

// FetchRegionOrdersPage fetches one page of GET
// /markets/{regionID}/orders/, sending If-None-Match: ifNoneMatch if
// non-empty, and backing off and retrying on 429 (rate limit) and 420
// (error limit) per ESI's politeness rules
// (docs/research/eve-market-mechanics-and-esi.md \u00a76.6): a 429's
// Retry-After header (seconds) sets the backoff if present, otherwise a
// fixed default is used. It gives up after maxBackoffAttempts and returns
// an error.
func (c *Client) FetchRegionOrdersPage(ctx context.Context, regionID int32, orderType string, typeID int32, page int, ifNoneMatch string) (PageResult, error) {
	q := url.Values{}
	q.Set("order_type", orderType)
	q.Set("page", strconv.Itoa(page))
	if typeID != 0 {
		q.Set("type_id", strconv.Itoa(int(typeID)))
	}
	reqURL := fmt.Sprintf("%s/markets/%d/orders/?%s", c.baseURL, regionID, q.Encode())

	resp, err := c.getWithBackoff(ctx, reqURL, ifNoneMatch)
	if err != nil {
		return PageResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return PageResult{NotModified: true, ETag: ifNoneMatch}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return PageResult{}, fmt.Errorf("GET %s: unexpected status %s", reqURL, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return PageResult{}, err
	}
	etag := resp.Header.Get("ETag")
	pages := 1
	if xp := resp.Header.Get("X-Pages"); xp != "" {
		if n, err := strconv.Atoi(xp); err == nil {
			pages = n
		}
	}

	return PageResult{Body: body, ETag: etag, Pages: pages}, nil
}

// getWithBackoff issues a GET to reqURL with the client's standard headers
// (X-Compatibility-Date, User-Agent, and If-None-Match: ifNoneMatch if
// non-empty), retrying on 429 (rate limit) and 420 (error limit) per ESI's
// politeness rules (docs/research/eve-market-mechanics-and-esi.md \u00a76.6): a
// 429's Retry-After header (seconds) sets the backoff if present, otherwise
// a fixed default is used. It gives up after maxBackoffAttempts and returns
// an error. It returns the first response with any other status (200, 304,
// or otherwise) for the caller to interpret; the caller is responsible for
// closing the response body.
func (c *Client) getWithBackoff(ctx context.Context, reqURL, ifNoneMatch string) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		if c.compatDate != "" {
			req.Header.Set("X-Compatibility-Date", c.compatDate)
		}
		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == statusErrorLimited {
			retryAfter := retryAfterDuration(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			if attempt >= maxBackoffAttempts {
				return nil, fmt.Errorf("GET %s: status %s after %d attempts", reqURL, resp.Status, attempt)
			}
			c.sleep(retryAfter)
			continue
		}

		return resp, nil
	}
}

// statusErrorLimited is ESI's 420 "Enhance Your Calm", returned once the
// error-limit budget (100 non-2xx/3xx per 60s) is exhausted
// (docs/research/eve-market-mechanics-and-esi.md \u00a76.6). net/http has no
// named constant for it.
const statusErrorLimited = 420

// retryAfterDuration parses a Retry-After header value (seconds) into a
// duration, falling back to defaultBackoff if it is missing or malformed.
func retryAfterDuration(header string) time.Duration {
	if header == "" {
		return defaultBackoff
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return defaultBackoff
	}
	return time.Duration(seconds) * time.Second
}
