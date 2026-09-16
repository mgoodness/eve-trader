// Package esi defines the seam through which eve-trader makes every
// outbound call to CCP's ESI API and EVE SSO OAuth endpoints. Callers
// depend only on the ESIGateway interface; a real implementation talks to
// ESI over HTTP, and a fake implementation (see fake.go) backs tests.
package esi

import (
	"context"
	"time"
)

// Order is a single order in Rens's order book, as returned by
// GET /markets/{region_id}/orders/ and filtered to Rens's location_id.
type Order struct {
	OrderID int64
	TypeID  int
	// Name is optional; gateways that do not provide it leave it empty and
	// the poller uses a stable fallback until a type lookup is available.
	Name         string
	IsBuyOrder   bool
	Price        float64
	VolumeRemain int
	VolumeTotal  int
	MinVolume    int
	Issued       time.Time
	Duration     int
}

// HistoryPoint is one day's Heimatar-region trading volume for a type_id,
// as returned by GET /markets/{region_id}/history/.
type HistoryPoint struct {
	Date       time.Time
	Volume     int
	OrderCount int
}

// Skills holds the fee/tax-relevant skill levels for a character: the
// active_skill_level (not trained_skill_level) of Broker Relations
// (skill ID 3446) and Accounting (skill ID 16622). A missing skill ID
// resolves to level 0.
type Skills struct {
	BrokerRelationsLevel int
	AccountingLevel      int
}

// Token is an EVE SSO OAuth token response.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	CharacterID  int
	OwnerHash    string
}

// ESIGateway is the seam through which eve-trader makes every outbound
// call to ESI and EVE SSO. All other application code is tested against
// this interface rather than against real HTTP calls.
type ESIGateway interface {
	// FetchRensOrders returns Rens's current order book.
	FetchRensOrders(ctx context.Context) ([]Order, error)

	// FetchHistory returns the Heimatar-region daily trading history for
	// the given type_id.
	FetchHistory(ctx context.Context, typeID int) ([]HistoryPoint, error)

	// FetchCharacterSkills returns the fee/tax-relevant skill levels for
	// the given character.
	FetchCharacterSkills(ctx context.Context, characterID int, accessToken string) (Skills, error)

	// ExchangeCode exchanges a PKCE authorization code (and its verifier)
	// for an EVE SSO token.
	ExchangeCode(ctx context.Context, code, verifier string) (Token, error)

	// RefreshToken exchanges a refresh token for a new EVE SSO token.
	RefreshToken(ctx context.Context, refreshToken string) (Token, error)
}
