package esi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errNotFound signals a 404 response, distinct from doJSON's generic
// "unexpected status" error so a caller like MarketOrders can tell "past
// the last page" (ESI's actual behavior -- confirmed live) apart from a
// real failure.
var errNotFound = errors.New("not found")

const (
	defaultTokenURL    = "https://login.eveonline.com/v2/oauth/token"
	defaultDataBaseURL = "https://esi.evetech.net/latest"

	// resolveNamesMaxIDs is ESI's documented per-call limit for
	// /universe/names/.
	resolveNamesMaxIDs = 1000
)

// TokenSource returns a valid access token for authenticated ESI calls,
// refreshing it first if necessary (see internal/auth.NewTokenSource).
type TokenSource func(ctx context.Context) (string, error)

// RealClient talks to login.eveonline.com (OAuth) and esi.evetech.net
// (ESI data).
type RealClient struct {
	HTTPClient   *http.Client
	ClientID     string
	ClientSecret string

	// TokenURL is the SSO token endpoint. Defaults to the real EVE SSO
	// endpoint; overridable in tests.
	TokenURL string

	// DataBaseURL is the ESI data API base URL. Defaults to the real ESI
	// endpoint; overridable in tests.
	DataBaseURL string

	// Tokens supplies the access token for authenticated ESI calls
	// (character orders/history). Must be set before calling those
	// methods; unauthenticated calls (ResolveNames) don't need it.
	Tokens TokenSource
}

var _ Client = (*RealClient)(nil)

// NewRealClient builds a RealClient authenticating as the given ESI
// application (Client ID/Secret from the developer application at
// developers.eveonline.com).
func NewRealClient(clientID, clientSecret string) *RealClient {
	return &RealClient{
		HTTPClient:   &http.Client{Timeout: 15 * time.Second},
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     defaultTokenURL,
		DataBaseURL:  defaultDataBaseURL,
	}
}

// ExchangeCode completes an OAuth authorization-code exchange.
func (c *RealClient) ExchangeCode(ctx context.Context, code string) (Token, error) {
	return c.tokenRequest(ctx, url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
	})
}

// RefreshAccessToken redeems a refresh token for a new access token. Per
// EVE SSO's documented behavior, the returned refresh token may differ
// from the one passed in -- callers must persist the newest value.
func (c *RealClient) RefreshAccessToken(ctx context.Context, refreshToken string) (Token, error) {
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *RealClient) tokenRequest(ctx context.Context, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.ClientID, c.ClientSecret)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Token{}, fmt.Errorf("token request: unexpected status %s: %s", resp.Status, detail)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}

	return Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		TokenType:    body.TokenType,
		ExpiresIn:    time.Duration(body.ExpiresIn) * time.Second,
	}, nil
}

// orderDTO mirrors ESI's order JSON shape (shared by the open-orders and
// order-history endpoints, which differ only by the history-only `state`
// field).
type orderDTO struct {
	OrderID       int64     `json:"order_id"`
	TypeID        int32     `json:"type_id"`
	RegionID      int32     `json:"region_id"`
	LocationID    int64     `json:"location_id"`
	IsBuyOrder    bool      `json:"is_buy_order"`
	IsCorporation bool      `json:"is_corporation"`
	Price         float64   `json:"price"`
	VolumeTotal   int32     `json:"volume_total"`
	VolumeRemain  int32     `json:"volume_remain"`
	MinVolume     int32     `json:"min_volume"`
	Duration      int32     `json:"duration"`
	Issued        time.Time `json:"issued"`
	Escrow        float64   `json:"escrow"`
	Range         string    `json:"range"`
	State         string    `json:"state"`
}

func (d orderDTO) toOrder() Order {
	return Order{
		OrderID:       d.OrderID,
		TypeID:        d.TypeID,
		RegionID:      d.RegionID,
		LocationID:    d.LocationID,
		IsBuyOrder:    d.IsBuyOrder,
		IsCorporation: d.IsCorporation,
		Price:         d.Price,
		VolumeTotal:   d.VolumeTotal,
		VolumeRemain:  d.VolumeRemain,
		MinVolume:     d.MinVolume,
		Duration:      d.Duration,
		Issued:        d.Issued,
		Escrow:        d.Escrow,
		Range:         d.Range,
	}
}

// CharacterOrders lists a character's open market orders.
func (c *RealClient) CharacterOrders(ctx context.Context, characterID int32) ([]Order, error) {
	var dtos []orderDTO
	path := fmt.Sprintf("/characters/%d/orders/", characterID)
	if err := c.authenticatedGet(ctx, path, nil, &dtos); err != nil {
		return nil, fmt.Errorf("character orders: %w", err)
	}

	orders := make([]Order, len(dtos))
	for i, d := range dtos {
		orders[i] = d.toOrder()
	}
	return orders, nil
}

// CharacterOrderHistory lists a character's cancelled/expired orders.
func (c *RealClient) CharacterOrderHistory(ctx context.Context, characterID int32, page int32) ([]OrderHistoryEntry, error) {
	var dtos []orderDTO
	path := fmt.Sprintf("/characters/%d/orders/history/", characterID)
	query := url.Values{"page": {fmt.Sprintf("%d", page)}}
	if err := c.authenticatedGet(ctx, path, query, &dtos); err != nil {
		return nil, fmt.Errorf("character order history: %w", err)
	}

	entries := make([]OrderHistoryEntry, len(dtos))
	for i, d := range dtos {
		entries[i] = OrderHistoryEntry{Order: d.toOrder(), State: d.State}
	}
	return entries, nil
}

// authenticatedGet performs a GET against the ESI data API with a bearer
// token from c.Tokens, decoding a JSON response into out.
func (c *RealClient) authenticatedGet(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.Tokens(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	reqURL := c.DataBaseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	return c.doJSON(req, out)
}

func (c *RealClient) doJSON(req *http.Request, out any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, detail)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// marketOrderDTO mirrors ESI's public regional order-book JSON shape.
type marketOrderDTO struct {
	OrderID      int64     `json:"order_id"`
	TypeID       int32     `json:"type_id"`
	LocationID   int64     `json:"location_id"`
	SystemID     int32     `json:"system_id"`
	IsBuyOrder   bool      `json:"is_buy_order"`
	Price        float64   `json:"price"`
	VolumeTotal  int32     `json:"volume_total"`
	VolumeRemain int32     `json:"volume_remain"`
	MinVolume    int32     `json:"min_volume"`
	Duration     int32     `json:"duration"`
	Issued       time.Time `json:"issued"`
	Range        string    `json:"range"`
}

// MarketOrders lists orders in a region (public endpoint, no auth
// required), one page at a time.
func (c *RealClient) MarketOrders(ctx context.Context, regionID int32, orderType OrderType, page int32) ([]MarketOrder, error) {
	var dtos []marketOrderDTO
	path := fmt.Sprintf("/markets/%d/orders/", regionID)
	query := url.Values{
		"order_type": {string(orderType)},
		"page":       {fmt.Sprintf("%d", page)},
	}
	if err := c.get(ctx, path, query, &dtos); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("market orders: %w", err)
	}

	orders := make([]MarketOrder, len(dtos))
	for i, d := range dtos {
		orders[i] = MarketOrder{
			OrderID:      d.OrderID,
			TypeID:       d.TypeID,
			LocationID:   d.LocationID,
			SystemID:     d.SystemID,
			IsBuyOrder:   d.IsBuyOrder,
			Price:        d.Price,
			VolumeTotal:  d.VolumeTotal,
			VolumeRemain: d.VolumeRemain,
			MinVolume:    d.MinVolume,
			Duration:     d.Duration,
			Issued:       d.Issued,
			Range:        d.Range,
		}
	}
	return orders, nil
}

// marketHistoryDTO mirrors ESI's public regional market-history JSON
// shape.
type marketHistoryDTO struct {
	Date       string  `json:"date"`
	OrderCount int64   `json:"order_count"`
	Volume     int64   `json:"volume"`
	Highest    float64 `json:"highest"`
	Lowest     float64 `json:"lowest"`
	Average    float64 `json:"average"`
}

// MarketHistory returns daily market statistics for one item type in a
// region (public endpoint, no auth required).
func (c *RealClient) MarketHistory(ctx context.Context, regionID int32, typeID int32) ([]MarketHistoryEntry, error) {
	var dtos []marketHistoryDTO
	path := fmt.Sprintf("/markets/%d/history/", regionID)
	query := url.Values{"type_id": {fmt.Sprintf("%d", typeID)}}
	if err := c.get(ctx, path, query, &dtos); err != nil {
		return nil, fmt.Errorf("market history: %w", err)
	}

	entries := make([]MarketHistoryEntry, len(dtos))
	for i, d := range dtos {
		date, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			return nil, fmt.Errorf("market history: parse date %q: %w", d.Date, err)
		}
		entries[i] = MarketHistoryEntry{
			Date:       date,
			OrderCount: d.OrderCount,
			Volume:     d.Volume,
			Highest:    d.Highest,
			Lowest:     d.Lowest,
			Average:    d.Average,
		}
	}
	return entries, nil
}

// get performs an unauthenticated GET against the ESI data API,
// decoding a JSON response into out.
func (c *RealClient) get(ctx context.Context, path string, query url.Values, out any) error {
	reqURL := c.DataBaseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	return c.doJSON(req, out)
}

// skillsDTO mirrors ESI's character-skills JSON shape.
type skillsDTO struct {
	TotalSP int64 `json:"total_sp"`
	Skills  []struct {
		SkillID           int32 `json:"skill_id"`
		ActiveSkillLevel  int32 `json:"active_skill_level"`
		TrainedSkillLevel int32 `json:"trained_skill_level"`
	} `json:"skills"`
}

// CharacterSkills lists a character's trained skills.
func (c *RealClient) CharacterSkills(ctx context.Context, characterID int32) (Skills, error) {
	var dto skillsDTO
	path := fmt.Sprintf("/characters/%d/skills/", characterID)
	if err := c.authenticatedGet(ctx, path, nil, &dto); err != nil {
		return Skills{}, fmt.Errorf("character skills: %w", err)
	}

	skills := Skills{TotalSP: dto.TotalSP, Skills: make([]Skill, len(dto.Skills))}
	for i, s := range dto.Skills {
		skills.Skills[i] = Skill{
			SkillID:           s.SkillID,
			ActiveSkillLevel:  s.ActiveSkillLevel,
			TrainedSkillLevel: s.TrainedSkillLevel,
		}
	}
	return skills, nil
}

// standingDTO mirrors one entry of ESI's character-standings JSON shape.
type standingDTO struct {
	FromID   int32   `json:"from_id"`
	FromType string  `json:"from_type"`
	Standing float64 `json:"standing"`
}

// CharacterStandings lists a character's NPC agent/corp/faction
// standings.
func (c *RealClient) CharacterStandings(ctx context.Context, characterID int32) ([]Standing, error) {
	var dtos []standingDTO
	path := fmt.Sprintf("/characters/%d/standings/", characterID)
	if err := c.authenticatedGet(ctx, path, nil, &dtos); err != nil {
		return nil, fmt.Errorf("character standings: %w", err)
	}

	standings := make([]Standing, len(dtos))
	for i, d := range dtos {
		standings[i] = Standing{FromID: d.FromID, FromType: d.FromType, Standing: d.Standing}
	}
	return standings, nil
}

// nameDTO mirrors one entry of ESI's universe/names response.
type nameDTO struct {
	ID       int32  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// ResolveNames resolves universe IDs to display names via ESI's bulk
// universe/names endpoint (public, no auth required), chunking requests at
// ESI's documented 1000-id-per-call limit.
func (c *RealClient) ResolveNames(ctx context.Context, ids []int32) (map[int32]string, error) {
	result := make(map[int32]string, len(ids))

	for start := 0; start < len(ids); start += resolveNamesMaxIDs {
		end := min(start+resolveNamesMaxIDs, len(ids))
		chunk := ids[start:end]

		body, err := json.Marshal(chunk)
		if err != nil {
			return nil, fmt.Errorf("resolve names: encode request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.DataBaseURL+"/universe/names/", strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("resolve names: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		var dtos []nameDTO
		if err := c.doJSON(req, &dtos); err != nil {
			return nil, fmt.Errorf("resolve names: %w", err)
		}

		for _, d := range dtos {
			result[d.ID] = d.Name
		}
	}

	return result, nil
}
