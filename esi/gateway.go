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
// It carries no display name: names are resolved separately, in batch,
// via FetchTypeNames so the order fetch stays a single pass over the
// region pages.
type Order struct {
	OrderID      int64
	TypeID       int
	IsBuyOrder   bool
	Price        float64
	VolumeRemain int
	VolumeTotal  int
	MinVolume    int
	Issued       time.Time
	Duration     int
}

// CharacterOrder is one of the character's own market orders, as returned
// by GET /characters/{character_id}/orders/ (open orders; State empty) and
// GET /characters/{character_id}/orders/history/ (cancelled and expired
// orders; State "cancelled" or "expired", ESI retaining ~90 days). Fully
// filled orders appear in neither route.
//
// ESI omits is_buy_order — and the buy-only min_volume and escrow fields —
// for sell orders on both routes, so a missing is_buy_order means a sell
// (false), which the gateway decodes explicitly. State, RegionID, Range and
// Escrow are zero where the route does not carry them.
type CharacterOrder struct {
	OrderID       int64
	TypeID        int
	LocationID    int64
	IsBuyOrder    bool
	Price         float64
	VolumeRemain  int
	VolumeTotal   int
	MinVolume     int
	Issued        time.Time
	Duration      int
	State         string
	IsCorporation bool
	RegionID      int
	Range         string
	Escrow        float64
}

// HistoryPoint is one day's Heimatar-region trading summary for a type_id,
// as returned by GET /markets/{region_id}/history/. Average, Highest and
// Lowest are the day's price figures; they are not optional because ESI
// always returns them for a day it reports at all.
type HistoryPoint struct {
	Date       time.Time
	Volume     int
	OrderCount int
	Average    float64
	Highest    float64
	Lowest     float64
}

// Skills holds the fee/tax-relevant skill levels for a character: the
// active_skill_level (not trained_skill_level) of Broker Relations
// (skill ID 3446), Accounting (skill ID 16622), and Advanced Broker
// Relations (skill ID 16597, the in-place re-list fee term; formerly
// Margin Trading). A missing skill ID resolves to level 0.
type Skills struct {
	BrokerRelationsLevel         int
	AccountingLevel              int
	AdvancedBrokerRelationsLevel int
}

// Token is an EVE SSO OAuth token response.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	CharacterID  int
	OwnerHash    string
}

// WalletTransaction is one entry from
// GET /characters/{character_id}/wallet/transactions/ -- a single buy or
// sell fill. JournalRefID is carried as ESI returns it but must never be
// used to link to WalletJournalEntry: it is unreliable (docs/spec/v2.md
// §3). Use WalletJournalEntry.ContextID instead.
type WalletTransaction struct {
	TransactionID int64
	Date          time.Time
	TypeID        int
	Quantity      int
	UnitPrice     float64
	IsBuy         bool
	IsPersonal    bool
	JournalRefID  int64
	LocationID    int64
	ClientID      int64
}

// WalletJournalEntry is one entry from
// GET /characters/{character_id}/wallet/journal/. For a ref_type of
// market_transaction, ContextID equals the matching WalletTransaction's
// TransactionID when ContextIDType is "market_transaction_id" -- the
// load-bearing link between the two streams (docs/spec/v2.md §3).
type WalletJournalEntry struct {
	ID            int64
	Date          time.Time
	RefType       string
	Amount        float64
	Balance       float64
	ContextID     int64
	ContextIDType string
	Description   string
	FirstPartyID  int
	SecondPartyID int
	Reason        string
	Tax           float64
	TaxReceiverID int
}

// Contract is one entry from
// GET /characters/{character_id}/contracts/ -- a contract the character
// issued, accepted, or is assigned. Only Type "item_exchange" carries
// goods and so can be a Transfer (docs/spec/v2.md §4.5); Price is the
// ISK the contract exchanges for those goods, zero for a pure handoff.
// StartLocationID/EndLocationID are populated for courier contracts only.
type Contract struct {
	ContractID          int64
	IssuerID            int64
	IssuerCorporationID int64
	AssigneeID          int64
	AcceptorID          int64
	Type                string
	Status              string
	Price               float64
	ForCorporation      bool
	DateIssued          time.Time
	DateExpired         time.Time
	DateCompleted       time.Time
	StartLocationID     int64
	EndLocationID       int64
	Title               string
}

// ContractItem is one entry from
// GET /characters/{character_id}/contracts/{contract_id}/items/ -- a
// stack of goods attached to a contract. IsIncluded distinguishes goods
// the issuer submitted (true, they leave the issuer) from goods the
// issuer asked for (false, they enter the issuer); IsSingleton marks a
// non-stackable item. It is the definitive record of what an
// item-exchange contract moved (docs/spec/v2.md §3).
type ContractItem struct {
	RecordID    int64
	TypeID      int
	Quantity    int
	IsSingleton bool
	IsIncluded  bool
}

// ESIGateway is the seam through which eve-trader makes every outbound
// call to ESI and EVE SSO. All other application code is tested against
// this interface rather than against real HTTP calls.
type ESIGateway interface {
	// FetchRensOrders returns Rens's current order book.
	FetchRensOrders(ctx context.Context) ([]Order, error)

	// FetchTypeNames resolves display names for the given type_ids in
	// batched universe-names requests. IDs the gateway cannot resolve are
	// omitted from the returned map rather than reported as an error.
	FetchTypeNames(ctx context.Context, typeIDs []int) (map[int]string, error)

	// FetchHistory returns the Heimatar-region daily trading history for
	// the given type_id.
	FetchHistory(ctx context.Context, typeID int) ([]HistoryPoint, error)

	// FetchCharacterSkills returns the fee/tax-relevant skill levels for
	// the given character.
	FetchCharacterSkills(ctx context.Context, characterID int, accessToken string) (Skills, error)

	// FetchWalletTransactions returns the character's wallet transactions.
	// fromID of zero fetches the current (most recent) page. A non-zero
	// fromID walks backward into older history; ESI's from_id boundary is
	// inclusive, so a caller paging backward must drop the entry matching
	// fromID itself from the returned page before treating it as new.
	FetchWalletTransactions(ctx context.Context, characterID int, accessToken string, fromID int64) ([]WalletTransaction, error)

	// FetchWalletJournal returns the character's whole wallet journal
	// (paginated internally; ESI has no from_id equivalent here, so every
	// call returns its full retained window).
	FetchWalletJournal(ctx context.Context, characterID int, accessToken string) ([]WalletJournalEntry, error)

	// FetchCharacterContracts returns the character's contracts -- those
	// they issued, accepted, or are assigned -- paginated internally over
	// ESI's ~30-day retained window (plus in-progress contracts).
	FetchCharacterContracts(ctx context.Context, characterID int, accessToken string) ([]Contract, error)

	// FetchContractItems returns one contract's items. ESI returns an
	// empty list for a contract type that carries no items (e.g. a
	// courier), so callers need not guard by contract type.
	FetchContractItems(ctx context.Context, characterID int, accessToken string, contractID int64) ([]ContractItem, error)

	// FetchCharacterOrders returns the character's open market orders.
	// ESI caches this route for ~20 minutes.
	FetchCharacterOrders(ctx context.Context, characterID int, accessToken string) ([]CharacterOrder, error)

	// FetchCharacterOrderHistory returns the character's cancelled and
	// expired market orders (paginated internally; ESI retains ~90 days).
	FetchCharacterOrderHistory(ctx context.Context, characterID int, accessToken string) ([]CharacterOrder, error)

	// ExchangeCode exchanges a PKCE authorization code (and its verifier)
	// for an EVE SSO token.
	ExchangeCode(ctx context.Context, code, verifier string) (Token, error)

	// RefreshToken exchanges a refresh token for a new EVE SSO token.
	RefreshToken(ctx context.Context, refreshToken string) (Token, error)
}
