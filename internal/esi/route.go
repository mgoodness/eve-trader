package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Route fetches GET /route/{origin}/{destination}/ (research
// eve-market-mechanics-and-esi.md §6.7), which returns the ordered list of
// solar system IDs along the shortest path from origin to destination,
// origin and destination inclusive. The jump distance between the two
// systems is len(route)-1. No auth/scope is required. Callers are
// responsible for caching the result (spec §6: 86,400 s).
func (c *Client) Route(ctx context.Context, origin, destination int32) ([]int32, error) {
	reqURL := fmt.Sprintf("%s/route/%d/%d/", c.baseURL, origin, destination)

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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s: %s", reqURL, resp.Status, body)
	}

	var route []int32
	if err := json.Unmarshal(body, &route); err != nil {
		return nil, fmt.Errorf("decoding route: %w", err)
	}
	return route, nil
}
