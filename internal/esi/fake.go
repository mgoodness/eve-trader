package esi

import (
	"context"
	"time"
)

// FakeClient is a canned-data Client for tests. Every field is exported so
// a test can override the canned response, and every method records its
// last call for assertions.
type FakeClient struct {
	TokenResp Token
	TokenErr  error

	Orders    []Order
	OrdersErr error

	OrderHistory    []OrderHistoryEntry
	OrderHistoryErr error

	MarketOrdersResp []MarketOrder
	MarketOrdersErr  error

	// MarketOrdersByType, if non-nil, overrides MarketOrdersResp: a call
	// returns MarketOrdersByType[typeID] instead of always the same
	// canned slice, so a test can verify typeID is actually plumbed
	// through by call site.
	MarketOrdersByType map[int32][]MarketOrder

	MarketHistoryResp []MarketHistoryEntry
	MarketHistoryErr  error

	SkillsResp Skills
	SkillsErr  error

	StandingsResp []Standing
	StandingsErr  error

	Names    map[int32]string
	NamesErr error
}

var _ Client = (*FakeClient)(nil)

// NewFakeClient returns a FakeClient pre-populated with plausible canned
// data for a single fake character trading at Jita.
func NewFakeClient() *FakeClient {
	issued := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	return &FakeClient{
		TokenResp: Token{
			AccessToken:  "fake-access-token",
			RefreshToken: "fake-refresh-token",
			TokenType:    "Bearer",
			ExpiresIn:    20 * time.Minute,
		},
		Orders: []Order{
			{
				OrderID:      1,
				TypeID:       34, // Tritanium
				RegionID:     10000002,
				LocationID:   60003760, // Jita IV - Moon 4
				IsBuyOrder:   false,
				Price:        5.5,
				VolumeTotal:  10000,
				VolumeRemain: 8000,
				MinVolume:    1,
				Duration:     90,
				Issued:       issued,
			},
		},
		OrderHistory: []OrderHistoryEntry{
			{
				Order: Order{
					OrderID:      2,
					TypeID:       35, // Pyerite
					RegionID:     10000002,
					LocationID:   60003760,
					IsBuyOrder:   true,
					Price:        12.0,
					VolumeTotal:  5000,
					VolumeRemain: 0,
					MinVolume:    1,
					Duration:     30,
					Issued:       issued.AddDate(0, 0, -30),
				},
				State: "expired",
			},
		},
		MarketOrdersResp: []MarketOrder{
			{
				OrderID:      100,
				TypeID:       34,
				LocationID:   60003760,
				SystemID:     30000142,
				IsBuyOrder:   false,
				Price:        5.4,
				VolumeTotal:  50000,
				VolumeRemain: 40000,
				MinVolume:    1,
				Duration:     90,
				Issued:       issued,
			},
		},
		MarketHistoryResp: []MarketHistoryEntry{
			{
				Date:       issued.AddDate(0, 0, -1),
				OrderCount: 120,
				Volume:     1_000_000,
				Highest:    5.6,
				Lowest:     5.3,
				Average:    5.45,
			},
		},
		SkillsResp: Skills{
			TotalSP: 50_000_000,
			Skills: []Skill{
				{SkillID: 16622, ActiveSkillLevel: 5, TrainedSkillLevel: 5}, // Accounting
				{SkillID: 3446, ActiveSkillLevel: 4, TrainedSkillLevel: 4},  // Broker Relations
			},
		},
		StandingsResp: []Standing{
			{FromID: 1000035, FromType: "npc_corp", Standing: 5.0}, // Caldari Navy (Jita's station owner)
			{FromID: 500001, FromType: "faction", Standing: 2.0},   // Caldari State (Jita's owning faction)
			{FromID: 1000049, FromType: "npc_corp", Standing: 3.0}, // Brutor Tribe (Rens's station owner)
			{FromID: 500002, FromType: "faction", Standing: 1.0},   // Minmatar Republic (Rens's owning faction)
		},
		Names: map[int32]string{
			34: "Tritanium",
			35: "Pyerite",
		},
	}
}

func (f *FakeClient) ExchangeCode(ctx context.Context, code string) (Token, error) {
	return f.TokenResp, f.TokenErr
}

func (f *FakeClient) RefreshAccessToken(ctx context.Context, refreshToken string) (Token, error) {
	return f.TokenResp, f.TokenErr
}

func (f *FakeClient) CharacterOrders(ctx context.Context, characterID int32) ([]Order, error) {
	return f.Orders, f.OrdersErr
}

func (f *FakeClient) CharacterOrderHistory(ctx context.Context, characterID int32, page int32) ([]OrderHistoryEntry, error) {
	if page > 1 {
		return nil, f.OrderHistoryErr
	}
	return f.OrderHistory, f.OrderHistoryErr
}

func (f *FakeClient) MarketOrders(ctx context.Context, regionID int32, orderType OrderType, typeID int32, page int32) ([]MarketOrder, error) {
	if page > 1 {
		return nil, f.MarketOrdersErr
	}
	if f.MarketOrdersByType != nil {
		return f.MarketOrdersByType[typeID], f.MarketOrdersErr
	}
	return f.MarketOrdersResp, f.MarketOrdersErr
}

func (f *FakeClient) MarketHistory(ctx context.Context, regionID int32, typeID int32) ([]MarketHistoryEntry, error) {
	return f.MarketHistoryResp, f.MarketHistoryErr
}

func (f *FakeClient) CharacterSkills(ctx context.Context, characterID int32) (Skills, error) {
	return f.SkillsResp, f.SkillsErr
}

func (f *FakeClient) CharacterStandings(ctx context.Context, characterID int32) ([]Standing, error) {
	return f.StandingsResp, f.StandingsErr
}

func (f *FakeClient) ResolveNames(ctx context.Context, ids []int32) (map[int32]string, error) {
	if f.NamesErr != nil {
		return nil, f.NamesErr
	}
	result := make(map[int32]string, len(ids))
	for _, id := range ids {
		if name, ok := f.Names[id]; ok {
			result[id] = name
		}
	}
	return result, nil
}
