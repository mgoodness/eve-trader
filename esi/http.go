package esi

import (
	"bytes"
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

	// RensStationID is the NPC station where every trade in scope takes
	// place: Rens VI - Moon 8 - Brutor Tribe Treasury, in Heimatar. It is
	// exported because the broker-fee standings term (docs/spec/v2.md §9)
	// resolves this station's owner and marries the owner standings to the
	// fee the ranking and portfolio both compute.
	RensStationID = 60004588

	// errorLimitedStatus is ESI's 420 "error limited" response, distinct from
	// the standard 429. Both mean the same thing to callers.
	errorLimitedStatus = 420
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
		resp, err := g.get(ctx, u, "", &raw)
		if err != nil {
			return nil, err
		}
		for _, v := range raw {
			if v.Location == RensStationID {
				out = append(out, Order{OrderID: v.OrderID, TypeID: v.TypeID, IsBuyOrder: v.IsBuy, Price: v.Price, VolumeRemain: v.Remain, VolumeTotal: v.Total, MinVolume: v.Min, Issued: v.Issued, Duration: v.Duration})
			}
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	return out, nil
}

// typeNamesBatchLimit is ESI's cap on IDs per POST /universe/names/
// request. Batching up to this many keeps name resolution to a handful of
// requests instead of one per distinct type in the order book.
const typeNamesBatchLimit = 1000

// FetchTypeNames resolves display names for typeIDs with batched
// POST /universe/names/ requests, at most typeNamesBatchLimit IDs each.
// IDs absent from ESI's response are omitted from the result.
func (g *HTTPGateway) FetchTypeNames(ctx context.Context, typeIDs []int) (map[int]string, error) {
	names := make(map[int]string, len(typeIDs))
	for start := 0; start < len(typeIDs); start += typeNamesBatchLimit {
		end := min(start+typeNamesBatchLimit, len(typeIDs))
		body, err := json.Marshal(typeIDs[start:end])
		if err != nil {
			return nil, fmt.Errorf("encoding type IDs: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base()+"/universe/names/", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		var raw []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		if err := g.doJSON(req, &raw); err != nil {
			return nil, fmt.Errorf("fetching type names: %w", err)
		}
		for _, v := range raw {
			names[v.ID] = v.Name
		}
	}
	return names, nil
}

func (g *HTTPGateway) get(ctx context.Context, endpoint, accessToken string, target any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return nil, err
		}
		return resp, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == errorLimitedStatus {
		return nil, newRateLimited(resp)
	}
	return nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
}

// FetchHistory returns Heimatar's daily market history for typeID.
func (g *HTTPGateway) FetchHistory(ctx context.Context, typeID int) ([]HistoryPoint, error) {
	var raw []struct {
		Date       string  `json:"date"`
		Volume     int     `json:"volume"`
		OrderCount int     `json:"order_count"`
		Average    float64 `json:"average"`
		Highest    float64 `json:"highest"`
		Lowest     float64 `json:"lowest"`
	}
	_, err := g.get(ctx, fmt.Sprintf("%s/markets/%d/history/?type_id=%d", g.base(), regionHeimatar, typeID), "", &raw)
	if err != nil {
		return nil, fmt.Errorf("fetching history for type %d: %w", typeID, err)
	}
	out := make([]HistoryPoint, len(raw))
	for i, point := range raw {
		date, err := time.Parse("2006-01-02", point.Date)
		if err != nil {
			return nil, fmt.Errorf("parsing history date %q for type %d: %w", point.Date, typeID, err)
		}
		out[i] = HistoryPoint{
			Date:       date,
			Volume:     point.Volume,
			OrderCount: point.OrderCount,
			Average:    point.Average,
			Highest:    point.Highest,
			Lowest:     point.Lowest,
		}
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
		case 16597:
			skills.AdvancedBrokerRelationsLevel = skill.Active
		}
	}
	return skills, nil
}

// FetchCharacterStandings returns the character's NPC standings. The
// route returns a bare, unpaginated array (no page parameter), so one
// request fetches the whole set (docs/spec/v2.md §9).
func (g *HTTPGateway) FetchCharacterStandings(ctx context.Context, characterID int, accessToken string) ([]Standing, error) {
	endpoint := fmt.Sprintf("%s/characters/%d/standings/", g.base(), characterID)
	var raw []struct {
		FromID   int     `json:"from_id"`
		FromType string  `json:"from_type"`
		Standing float64 `json:"standing"`
	}
	if _, err := g.get(ctx, endpoint, accessToken, &raw); err != nil {
		return nil, fmt.Errorf("fetching standings for character %d: %w", characterID, err)
	}
	out := make([]Standing, len(raw))
	for i, v := range raw {
		out[i] = Standing{FromID: v.FromID, FromType: v.FromType, Standing: v.Standing}
	}
	return out, nil
}

// FetchStationOwner returns the corporation id that owns stationID, from
// the public GET /universe/stations/{id} `owner` field.
func (g *HTTPGateway) FetchStationOwner(ctx context.Context, stationID int64) (int64, error) {
	var raw struct {
		Owner int64 `json:"owner"`
	}
	if _, err := g.get(ctx, fmt.Sprintf("%s/universe/stations/%d/", g.base(), stationID), "", &raw); err != nil {
		return 0, fmt.Errorf("fetching station %d owner: %w", stationID, err)
	}
	return raw.Owner, nil
}

// FetchWalletTransactions returns the character's wallet transactions.
// fromID of zero fetches ESI's current page (the most recent
// transactions); a non-zero fromID walks backward into older history.
// ESI's from_id boundary is inclusive: a caller paging backward will see
// the transaction matching fromID again as the first element of the next
// page and must drop it.
func (g *HTTPGateway) FetchWalletTransactions(ctx context.Context, characterID int, accessToken string, fromID int64) ([]WalletTransaction, error) {
	endpoint := fmt.Sprintf("%s/characters/%d/wallet/transactions/", g.base(), characterID)
	if fromID > 0 {
		endpoint += fmt.Sprintf("?from_id=%d", fromID)
	}
	var raw []struct {
		TransactionID int64     `json:"transaction_id"`
		Date          time.Time `json:"date"`
		TypeID        int       `json:"type_id"`
		Quantity      int       `json:"quantity"`
		UnitPrice     float64   `json:"unit_price"`
		IsBuy         bool      `json:"is_buy"`
		IsPersonal    bool      `json:"is_personal"`
		JournalRefID  int64     `json:"journal_ref_id"`
		LocationID    int64     `json:"location_id"`
		ClientID      int64     `json:"client_id"`
	}
	if _, err := g.get(ctx, endpoint, accessToken, &raw); err != nil {
		return nil, fmt.Errorf("fetching wallet transactions for character %d: %w", characterID, err)
	}
	out := make([]WalletTransaction, len(raw))
	for i, v := range raw {
		out[i] = WalletTransaction{
			TransactionID: v.TransactionID,
			Date:          v.Date,
			TypeID:        v.TypeID,
			Quantity:      v.Quantity,
			UnitPrice:     v.UnitPrice,
			IsBuy:         v.IsBuy,
			IsPersonal:    v.IsPersonal,
			JournalRefID:  v.JournalRefID,
			LocationID:    v.LocationID,
			ClientID:      v.ClientID,
		}
	}
	return out, nil
}

// FetchWalletJournal returns the character's whole wallet journal, walking
// the page/X-Pages pagination exactly like FetchRensOrders.
func (g *HTTPGateway) FetchWalletJournal(ctx context.Context, characterID int, accessToken string) ([]WalletJournalEntry, error) {
	var out []WalletJournalEntry
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/characters/%d/wallet/journal/?page=%d", g.base(), characterID, page)
		var raw []struct {
			ID            int64     `json:"id"`
			Date          time.Time `json:"date"`
			RefType       string    `json:"ref_type"`
			Amount        float64   `json:"amount"`
			Balance       float64   `json:"balance"`
			ContextID     int64     `json:"context_id"`
			ContextIDType string    `json:"context_id_type"`
			Description   string    `json:"description"`
			FirstPartyID  int       `json:"first_party_id"`
			SecondPartyID int       `json:"second_party_id"`
			Reason        string    `json:"reason"`
			Tax           float64   `json:"tax"`
			TaxReceiverID int       `json:"tax_receiver_id"`
		}
		resp, err := g.get(ctx, endpoint, accessToken, &raw)
		if err != nil {
			return nil, fmt.Errorf("fetching wallet journal for character %d: %w", characterID, err)
		}
		for _, v := range raw {
			out = append(out, WalletJournalEntry{
				ID:            v.ID,
				Date:          v.Date,
				RefType:       v.RefType,
				Amount:        v.Amount,
				Balance:       v.Balance,
				ContextID:     v.ContextID,
				ContextIDType: v.ContextIDType,
				Description:   v.Description,
				FirstPartyID:  v.FirstPartyID,
				SecondPartyID: v.SecondPartyID,
				Reason:        v.Reason,
				Tax:           v.Tax,
				TaxReceiverID: v.TaxReceiverID,
			})
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	return out, nil
}

// FetchCharacterContracts returns the character's contracts, walking the
// page/X-Pages pagination like FetchWalletJournal.
func (g *HTTPGateway) FetchCharacterContracts(ctx context.Context, characterID int, accessToken string) ([]Contract, error) {
	var out []Contract
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/characters/%d/contracts/?page=%d", g.base(), characterID, page)
		var raw []struct {
			ContractID          int64     `json:"contract_id"`
			IssuerID            int64     `json:"issuer_id"`
			IssuerCorporationID int64     `json:"issuer_corporation_id"`
			AssigneeID          int64     `json:"assignee_id"`
			AcceptorID          int64     `json:"acceptor_id"`
			Type                string    `json:"type"`
			Status              string    `json:"status"`
			Price               float64   `json:"price"`
			ForCorporation      bool      `json:"for_corporation"`
			DateIssued          time.Time `json:"date_issued"`
			DateExpired         time.Time `json:"date_expired"`
			DateCompleted       time.Time `json:"date_completed"`
			StartLocationID     int64     `json:"start_location_id"`
			EndLocationID       int64     `json:"end_location_id"`
			Title               string    `json:"title"`
		}
		resp, err := g.get(ctx, endpoint, accessToken, &raw)
		if err != nil {
			return nil, fmt.Errorf("fetching contracts for character %d: %w", characterID, err)
		}
		for _, v := range raw {
			out = append(out, Contract{
				ContractID:          v.ContractID,
				IssuerID:            v.IssuerID,
				IssuerCorporationID: v.IssuerCorporationID,
				AssigneeID:          v.AssigneeID,
				AcceptorID:          v.AcceptorID,
				Type:                v.Type,
				Status:              v.Status,
				Price:               v.Price,
				ForCorporation:      v.ForCorporation,
				DateIssued:          v.DateIssued,
				DateExpired:         v.DateExpired,
				DateCompleted:       v.DateCompleted,
				StartLocationID:     v.StartLocationID,
				EndLocationID:       v.EndLocationID,
				Title:               v.Title,
			})
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	return out, nil
}

// FetchContractItems returns one contract's items, walking the
// page/X-Pages pagination like FetchWalletJournal.
func (g *HTTPGateway) FetchContractItems(ctx context.Context, characterID int, accessToken string, contractID int64) ([]ContractItem, error) {
	var out []ContractItem
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/characters/%d/contracts/%d/items/?page=%d", g.base(), characterID, contractID, page)
		var raw []struct {
			RecordID    int64 `json:"record_id"`
			TypeID      int   `json:"type_id"`
			Quantity    int   `json:"quantity"`
			IsSingleton bool  `json:"is_singleton"`
			IsIncluded  bool  `json:"is_included"`
		}
		resp, err := g.get(ctx, endpoint, accessToken, &raw)
		if err != nil {
			return nil, fmt.Errorf("fetching items for contract %d of character %d: %w", contractID, characterID, err)
		}
		for _, v := range raw {
			out = append(out, ContractItem{
				RecordID:    v.RecordID,
				TypeID:      v.TypeID,
				Quantity:    v.Quantity,
				IsSingleton: v.IsSingleton,
				IsIncluded:  v.IsIncluded,
			})
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	return out, nil
}

func (g *HTTPGateway) FetchCharacterOrders(ctx context.Context, characterID int, accessToken string) ([]CharacterOrder, error) {
	return g.fetchCharacterOrders(ctx, characterID, accessToken, "/characters/%d/orders/")
}

// FetchCharacterOrderHistory returns the character's cancelled and expired
// orders, walking the page/X-Pages pagination like FetchCharacterOrders.
func (g *HTTPGateway) FetchCharacterOrderHistory(ctx context.Context, characterID int, accessToken string) ([]CharacterOrder, error) {
	return g.fetchCharacterOrders(ctx, characterID, accessToken, "/characters/%d/orders/history/")
}

// fetchCharacterOrders pages an authenticated character-orders route.
// route is the unformatted path (it carries the character_id placeholder).
// is_buy_order is decoded as a pointer because ESI omits it for sell
// orders, where a missing field means false (docs/spec/v2.md §3).
func (g *HTTPGateway) fetchCharacterOrders(ctx context.Context, characterID int, accessToken, route string) ([]CharacterOrder, error) {
	var out []CharacterOrder
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s"+route+"?page=%d", g.base(), characterID, page)
		var raw []struct {
			OrderID       int64     `json:"order_id"`
			TypeID        int       `json:"type_id"`
			LocationID    int64     `json:"location_id"`
			IsBuyOrder    *bool     `json:"is_buy_order"`
			Price         float64   `json:"price"`
			VolumeRemain  int       `json:"volume_remain"`
			VolumeTotal   int       `json:"volume_total"`
			MinVolume     int       `json:"min_volume"`
			Issued        time.Time `json:"issued"`
			Duration      int       `json:"duration"`
			State         string    `json:"state"`
			IsCorporation bool      `json:"is_corporation"`
			RegionID      int       `json:"region_id"`
			Range         string    `json:"range"`
			Escrow        float64   `json:"escrow"`
		}
		resp, err := g.get(ctx, endpoint, accessToken, &raw)
		if err != nil {
			return nil, fmt.Errorf("fetching character %d orders: %w", characterID, err)
		}
		for _, v := range raw {
			out = append(out, CharacterOrder{
				OrderID:       v.OrderID,
				TypeID:        v.TypeID,
				LocationID:    v.LocationID,
				IsBuyOrder:    v.IsBuyOrder != nil && *v.IsBuyOrder,
				Price:         v.Price,
				VolumeRemain:  v.VolumeRemain,
				VolumeTotal:   v.VolumeTotal,
				MinVolume:     v.MinVolume,
				Issued:        v.Issued,
				Duration:      v.Duration,
				State:         v.State,
				IsCorporation: v.IsCorporation,
				RegionID:      v.RegionID,
				Range:         v.Range,
				Escrow:        v.Escrow,
			})
		}
		pages := resp.Header.Get("X-Pages")
		n, _ := strconv.Atoi(pages)
		if n == 0 || page >= n {
			break
		}
	}
	return out, nil
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
