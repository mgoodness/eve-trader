package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	regionHeimatar = 10000030
	rensStation    = 60004588
)

// HTTPGateway reads public market data from ESI. OAuth methods are supplied
// through the same gateway seam by the production auth integration.
type HTTPGateway struct {
	Client  *http.Client
	BaseURL string
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

// These methods are intentionally explicit until the authenticated ESI client
// is introduced; public market polling does not require OAuth.
func (*HTTPGateway) FetchHistory(context.Context, int) ([]HistoryPoint, error) {
	return nil, fmt.Errorf("history polling is not implemented")
}
func (*HTTPGateway) FetchCharacterSkills(context.Context, int, string) (Skills, error) {
	return Skills{}, fmt.Errorf("skill fetching is not implemented")
}
func (*HTTPGateway) ExchangeCode(context.Context, string, string) (Token, error) {
	return Token{}, fmt.Errorf("oauth is not implemented")
}
func (*HTTPGateway) RefreshToken(context.Context, string) (Token, error) {
	return Token{}, fmt.Errorf("oauth is not implemented")
}
