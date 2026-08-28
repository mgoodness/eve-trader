// Package esi defines the app's single testing seam: an interface covering
// every EVE ESI / SSO interaction the app needs, plus the data shapes those
// calls return. See docs/research/esi-sso-oauth-flow.md and
// docs/research/esi-market-endpoints.md for the source data.
package esi

import (
	"context"
	"time"
)

// Client is the app's one seam onto EVE's SSO and ESI APIs. A real
// implementation talks to login.eveonline.com and esi.evetech.net; a fake
// implementation (see FakeClient) returns canned data for tests.
type Client interface {
	// ExchangeCode completes an OAuth authorization-code exchange.
	ExchangeCode(ctx context.Context, code string) (Token, error)
	// RefreshAccessToken redeems a refresh token for a new access token.
	// The response's RefreshToken may differ from the one passed in —
	// callers must always persist the newest value (see ADR-0003).
	RefreshAccessToken(ctx context.Context, refreshToken string) (Token, error)

	// CharacterOrders lists a character's open market orders.
	CharacterOrders(ctx context.Context, characterID int32) ([]Order, error)
	// CharacterOrderHistory lists a character's cancelled/expired orders
	// from up to 90 days in the past, one page at a time (page is 1-based).
	CharacterOrderHistory(ctx context.Context, characterID int32, page int32) ([]OrderHistoryEntry, error)

	// MarketOrders lists orders in a region, one page at a time (page is
	// 1-based). The region-wide result must be filtered client-side by
	// LocationID to isolate a specific Hub.
	MarketOrders(ctx context.Context, regionID int32, orderType OrderType, page int32) ([]MarketOrder, error)
	// MarketHistory returns daily market statistics for one item type in a
	// region, refreshed once daily by ESI at 11:05 UTC.
	MarketHistory(ctx context.Context, regionID int32, typeID int32) ([]MarketHistoryEntry, error)

	// CharacterSkills lists a character's trained skills, used to compute
	// broker-fee/sales-tax rates (see ADR-0001).
	CharacterSkills(ctx context.Context, characterID int32) (Skills, error)
	// CharacterStandings lists a character's NPC corp/faction standings,
	// also used to compute broker-fee/sales-tax rates (see ADR-0001).
	CharacterStandings(ctx context.Context, characterID int32) ([]Standing, error)
}

// Token is the response shape from both the authorization-code exchange and
// the refresh-token grant.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    time.Duration
}

// OrderType selects which side of the book MarketOrders returns.
type OrderType string

const (
	OrderTypeBuy  OrderType = "buy"
	OrderTypeSell OrderType = "sell"
	OrderTypeAll  OrderType = "all"
)

// Order is one of a character's own open market orders
// (CharactersCharacterIdOrdersGet).
type Order struct {
	OrderID       int64
	TypeID        int32
	RegionID      int32
	LocationID    int64
	IsBuyOrder    bool
	IsCorporation bool
	Price         float64
	VolumeTotal   int32
	VolumeRemain  int32
	MinVolume     int32
	Duration      int32
	Issued        time.Time
	Escrow        float64
	Range         string
}

// OrderHistoryEntry is a cancelled or expired order
// (CharactersCharacterIdOrdersHistoryGet).
type OrderHistoryEntry struct {
	Order
	State string // "cancelled" or "expired"
}

// MarketOrder is one entry from a region's public order book
// (MarketsRegionIdOrdersGet).
type MarketOrder struct {
	OrderID      int64
	TypeID       int32
	LocationID   int64
	SystemID     int32
	IsBuyOrder   bool
	Price        float64
	VolumeTotal  int32
	VolumeRemain int32
	MinVolume    int32
	Duration     int32
	Issued       time.Time
	Range        string
}

// MarketHistoryEntry is one day of a region's market statistics for a type
// (MarketsRegionIdHistoryGet).
type MarketHistoryEntry struct {
	Date       time.Time
	OrderCount int64
	Volume     int64
	Highest    float64
	Lowest     float64
	Average    float64
}

// Skills is a character's trained skills.
type Skills struct {
	TotalSP int64
	Skills  []Skill
}

// Skill is one trained skill entry.
type Skill struct {
	SkillID           int32
	ActiveSkillLevel  int32
	TrainedSkillLevel int32
}

// Standing is a character's standing with an NPC agent, corporation, or
// faction.
type Standing struct {
	FromID   int32
	FromType string // "agent", "npc_corp", or "faction"
	Standing float64
}
