package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Standing is one entry of a character's standings toward an agent, NPC
// corporation, or faction (docs/research/eve-market-mechanics-and-esi.md
// §5, §6.5). Fees.DeriveFees needs the entries whose FromID matches the
// trade station's owning corporation and the region's faction.
type Standing struct {
	FromID   int32   `json:"from_id"`
	FromType string  `json:"from_type"`
	Standing float64 `json:"standing"`
}

type skillEntry struct {
	SkillID          int32 `json:"skill_id"`
	ActiveSkillLevel int   `json:"active_skill_level"`
}

type skillsResponse struct {
	Skills []skillEntry `json:"skills"`
}

// Skills fetches the character's trained skills (spec §4, §12; research
// §6.4) and returns them as active_skill_level by skill_id — the shape
// engine.DeriveFees and engine.OrderLimit want. active_skill_level is used
// deliberately, not trained_skill_level (research §6.4: it "can differ from
// trained due to alpha status and/or active expert systems").
func (c *Client) Skills(ctx context.Context, characterID int32, accessToken string) (map[int32]int, error) {
	reqURL := fmt.Sprintf("%s/characters/%d/skills/", c.baseURL, characterID)
	body, err := c.getAuthenticated(ctx, reqURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetching skills: %w", err)
	}

	var wire skillsResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decoding skills: %w", err)
	}

	skills := make(map[int32]int, len(wire.Skills))
	for _, s := range wire.Skills {
		skills[s.SkillID] = s.ActiveSkillLevel
	}
	return skills, nil
}

// Standings fetches the character's standings toward agents, NPC
// corporations, and factions (spec §4, §12; research §5, §6.5).
func (c *Client) Standings(ctx context.Context, characterID int32, accessToken string) ([]Standing, error) {
	reqURL := fmt.Sprintf("%s/characters/%d/standings/", c.baseURL, characterID)
	body, err := c.getAuthenticated(ctx, reqURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetching standings: %w", err)
	}

	var standings []Standing
	if err := json.Unmarshal(body, &standings); err != nil {
		return nil, fmt.Errorf("decoding standings: %w", err)
	}
	return standings, nil
}

// getAuthenticated performs a bearer-authenticated GET against an ESI
// route, setting the same X-Compatibility-Date/User-Agent headers as every
// other ESI call.
func (c *Client) getAuthenticated(ctx context.Context, reqURL, accessToken string) ([]byte, error) {
	body, _, err := c.getAuthenticatedWithHeaders(ctx, reqURL, accessToken)
	return body, err
}

// getAuthenticatedWithHeaders is getAuthenticated plus the response
// headers, so a paginated route (character assets/order history) can read
// X-Pages.
func (c *Client) getAuthenticatedWithHeaders(ctx context.Context, reqURL, accessToken string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if c.compatDate != "" {
		req.Header.Set("X-Compatibility-Date", c.compatDate)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GET %s: unexpected status %s: %s", reqURL, resp.Status, body)
	}
	return body, resp.Header, nil
}
