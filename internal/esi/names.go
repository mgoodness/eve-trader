package esi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// typeName mirrors one entry of the POST /universe/names/ wire format
// (research eve-market-mechanics-and-esi.md §6: the bulk name resolver).
type typeName struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
}

// Names resolves the given type ids to their display names via
// POST /universe/names/ (a public route: no SSO, no scope). ESI returns
// only the ids it can resolve, so an id missing from the result map is
// genuinely unresolved rather than an error. Callers are responsible for
// caching names (spec §6); they are effectively immutable.
func (c *Client) Names(ctx context.Context, typeIDs []int32) (map[int32]string, error) {
	reqURL := c.baseURL + "/universe/names/"

	requestBody, err := json.Marshal(typeIDs)
	if err != nil {
		return nil, fmt.Errorf("encoding type ids: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST %s: unexpected status %s: %s", reqURL, resp.Status, body)
	}

	var wire []typeName
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decoding type names: %w", err)
	}

	names := make(map[int32]string, len(wire))
	for _, n := range wire {
		names[n.ID] = n.Name
	}
	return names, nil
}
