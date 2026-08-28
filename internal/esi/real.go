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

const defaultTokenURL = "https://login.eveonline.com/v2/oauth/token"

// ErrNotImplemented is returned by RealClient methods that don't talk to
// ESI yet -- those endpoints (character orders/history, market orders/
// history, skills, standings) land in later tickets (#16 dashboard, #19
// scanner) rather than this OAuth-focused one.
var ErrNotImplemented = errors.New("esi: not implemented")

// RealClient talks to login.eveonline.com (OAuth) and, once later tickets
// fill in the remaining methods, esi.evetech.net (ESI data).
type RealClient struct {
	HTTPClient   *http.Client
	ClientID     string
	ClientSecret string

	// TokenURL is the SSO token endpoint. Defaults to the real EVE SSO
	// endpoint; overridable in tests.
	TokenURL string
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

func (c *RealClient) CharacterOrders(ctx context.Context, characterID int32) ([]Order, error) {
	return nil, ErrNotImplemented
}

func (c *RealClient) CharacterOrderHistory(ctx context.Context, characterID int32, page int32) ([]OrderHistoryEntry, error) {
	return nil, ErrNotImplemented
}

func (c *RealClient) MarketOrders(ctx context.Context, regionID int32, orderType OrderType, page int32) ([]MarketOrder, error) {
	return nil, ErrNotImplemented
}

func (c *RealClient) MarketHistory(ctx context.Context, regionID int32, typeID int32) ([]MarketHistoryEntry, error) {
	return nil, ErrNotImplemented
}

func (c *RealClient) CharacterSkills(ctx context.Context, characterID int32) (Skills, error) {
	return Skills{}, ErrNotImplemented
}

func (c *RealClient) CharacterStandings(ctx context.Context, characterID int32) ([]Standing, error) {
	return nil, ErrNotImplemented
}
