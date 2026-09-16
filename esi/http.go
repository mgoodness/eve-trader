package esi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	regionHeimatar = 10000030
	rensStation    = 60004588
)

// HTTPGateway reads public market data from ESI and implements the EVE SSO
// calls used by the server's authentication and skill-refresh flows.
type HTTPGateway struct {
	Client       *http.Client
	BaseURL      string
	OAuthBaseURL string
	ClientID     string
	CallbackURL  string
}

func (g *HTTPGateway) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}
func (g *HTTPGateway) base() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return "https://esi.evetech.net/latest"
}

func (g *HTTPGateway) FetchRensOrders(ctx context.Context) ([]Order, error) {
	var out []Order
	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/markets/%d/orders/?order_type=all&page=%d", g.base(), regionHeimatar, page)
		var raw []struct {
			OrderID  int64     `json:"order_id"`
			TypeID   int       `json:"type_id"`
			IsBuy    bool      `json:"is_buy_order"`
			Price    float64   `json:"price"`
			Remain   int       `json:"volume_remain"`
			Total    int       `json:"volume_total"`
			Min      int       `json:"min_volume"`
			Issued   time.Time `json:"issued"`
			Duration int       `json:"duration"`
			Location int       `json:"location_id"`
		}
		resp, err := g.get(ctx, u, &raw)
		if err != nil {
			return nil, err
		}
		for _, v := range raw {
			if v.Location == rensStation {
				out = append(out, Order{OrderID: v.OrderID, TypeID: v.TypeID, IsBuyOrder: v.IsBuy, Price: v.Price, VolumeRemain: v.Remain, VolumeTotal: v.Total, MinVolume: v.Min, Issued: v.Issued, Duration: v.Duration})
			}
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	names := make(map[int]string)
	for i := range out {
		name, ok := names[out[i].TypeID]
		if !ok {
			var err error
			name, err = g.fetchTypeName(ctx, out[i].TypeID)
			if err != nil {
				return nil, err
			}
			names[out[i].TypeID] = name
		}
		out[i].Name = name
	}
	return out, nil
}

func (g *HTTPGateway) fetchTypeName(ctx context.Context, typeID int) (string, error) {
	var typeData struct {
		Name string `json:"name"`
	}
	_, err := g.get(ctx, fmt.Sprintf("%s/universe/types/%d/", g.base(), typeID), &typeData)
	if err != nil {
		return "", fmt.Errorf("fetching type %d name: %w", typeID, err)
	}
	return typeData.Name, nil
}

func (g *HTTPGateway) get(ctx context.Context, endpoint string, target any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESI %s: %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return nil, err
	}
	return resp, nil
}

// FetchHistory returns Heimatar's daily market history for typeID.
func (g *HTTPGateway) FetchHistory(ctx context.Context, typeID int) ([]HistoryPoint, error) {
	var raw []struct {
		Date       string `json:"date"`
		Volume     int    `json:"volume"`
		OrderCount int    `json:"order_count"`
	}
	_, err := g.get(ctx, fmt.Sprintf("%s/markets/%d/history/?type_id=%d", g.base(), regionHeimatar, typeID), &raw)
	if err != nil {
		return nil, fmt.Errorf("fetching history for type %d: %w", typeID, err)
	}
	out := make([]HistoryPoint, len(raw))
	for i, point := range raw {
		date, err := time.Parse("2006-01-02", point.Date)
		if err != nil {
			return nil, fmt.Errorf("parsing history date %q for type %d: %w", point.Date, typeID, err)
		}
		out[i] = HistoryPoint{Date: date, Volume: point.Volume, OrderCount: point.OrderCount}
	}
	return out, nil
}
func (g *HTTPGateway) FetchCharacterSkills(ctx context.Context, characterID int, accessToken string) (Skills, error) {
	var raw struct {
		Skills []struct {
			ID     int `json:"skill_id"`
			Active int `json:"active_skill_level"`
		} `json:"skills"`
	}
	endpoint := fmt.Sprintf("%s/characters/%d/skills/", g.base(), characterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Skills{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := g.doJSON(req, &raw); err != nil {
		return Skills{}, fmt.Errorf("fetching skills for character %d: %w", characterID, err)
	}
	var skills Skills
	for _, skill := range raw.Skills {
		switch skill.ID {
		case 3446:
			skills.BrokerRelationsLevel = skill.Active
		case 16622:
			skills.AccountingLevel = skill.Active
		}
	}
	return skills, nil
}

func (g *HTTPGateway) ExchangeCode(ctx context.Context, code, verifier string) (Token, error) {
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {g.ClientID},
		"redirect_uri":  {g.CallbackURL},
		"code_verifier": {verifier},
	}
	return g.exchangeToken(ctx, values)
}

func (g *HTTPGateway) RefreshToken(ctx context.Context, refreshToken string) (Token, error) {
	return g.exchangeToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {g.ClientID},
	})
}

func (g *HTTPGateway) exchangeToken(ctx context.Context, values url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL(), strings.NewReader(values.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := g.doJSON(req, &raw); err != nil {
		return Token{}, fmt.Errorf("exchanging EVE SSO token: %w", err)
	}
	token := Token{AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken, ExpiresIn: time.Duration(raw.ExpiresIn) * time.Second}
	claims, err := jwtClaims(raw.AccessToken)
	if err != nil {
		return Token{}, fmt.Errorf("decoding EVE access token: %w", err)
	}
	const subjectPrefix = "CHARACTER:EVE:"
	if !strings.HasPrefix(claims.Subject, subjectPrefix) {
		return Token{}, fmt.Errorf("unexpected EVE token subject %q", claims.Subject)
	}
	token.CharacterID, err = strconv.Atoi(strings.TrimPrefix(claims.Subject, subjectPrefix))
	if err != nil {
		return Token{}, fmt.Errorf("parsing EVE character ID: %w", err)
	}
	token.OwnerHash = claims.Owner
	return token, nil
}

func (g *HTTPGateway) tokenURL() string {
	if g.OAuthBaseURL != "" {
		return strings.TrimRight(g.OAuthBaseURL, "/") + "/oauth/token"
	}
	return "https://login.eveonline.com/v2/oauth/token"
}

func (g *HTTPGateway) doJSON(req *http.Request, target any) error {
	resp, err := g.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("EVE API: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

type jwtClaimsPayload struct {
	Subject string `json:"sub"`
	Owner   string `json:"owner"`
}

func jwtClaims(token string) (jwtClaimsPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaimsPayload{}, fmt.Errorf("malformed JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaimsPayload{}, err
	}
	var claims jwtClaimsPayload
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaimsPayload{}, err
	}
	return claims, nil
}
