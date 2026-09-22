package esi_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

func TestFakeFetchRegionOrdersReturnsSeededOrders(t *testing.T) {
	want := []esi.Order{
		{OrderID: 1, TypeID: 34, LocationID: 60004588, IsBuyOrder: false, Price: 5.5, VolumeRemain: 100},
	}
	fake := &esi.Fake{Orders: want}

	got, err := fake.FetchRegionOrders(t.Context())
	if err != nil {
		t.Fatalf("FetchRegionOrders() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FetchRegionOrders() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchRegionOrdersReturnsSeededError(t *testing.T) {
	wantErr := errors.New("esi down")
	fake := &esi.Fake{FetchOrdersErr: wantErr}

	_, err := fake.FetchRegionOrders(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("FetchRegionOrders() error = %v, want %v", err, wantErr)
	}
}

func TestFakeFetchHistoryReturnsSeededPointsByTypeID(t *testing.T) {
	pts := []esi.HistoryPoint{{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Volume: 42, OrderCount: 3, Average: 5.5, Highest: 6.0, Lowest: 5.0}}
	fake := &esi.Fake{History: map[int][]esi.HistoryPoint{34: pts}}

	got, err := fake.FetchHistory(t.Context(), 34)
	if err != nil {
		t.Fatalf("FetchHistory() error = %v", err)
	}
	if len(got) != 1 || got[0] != pts[0] {
		t.Fatalf("FetchHistory() = %+v, want %+v", got, pts)
	}

	empty, err := fake.FetchHistory(t.Context(), 999)
	if err != nil {
		t.Fatalf("FetchHistory() error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("FetchHistory() for unseeded type_id = %+v, want empty", empty)
	}
}

func TestFakeFetchHistoryCanProduceRateLimited(t *testing.T) {
	want := &esi.RateLimited{RetryAfter: 3 * time.Second}
	fake := &esi.Fake{FetchHistoryErr: want}

	_, err := fake.FetchHistory(t.Context(), 34)
	var rateLimited *esi.RateLimited
	if !errors.As(err, &rateLimited) {
		t.Fatalf("FetchHistory() error = %v, want *esi.RateLimited", err)
	}
	if rateLimited.RetryAfter != want.RetryAfter {
		t.Fatalf("RetryAfter = %v, want %v", rateLimited.RetryAfter, want.RetryAfter)
	}
}

func TestFakeFetchTypeNamesReturnsSeededNamesByID(t *testing.T) {
	fake := &esi.Fake{TypeNames: map[int]string{34: "Tritanium", 35: "Pyerite"}}

	got, err := fake.FetchTypeNames(t.Context(), []int{34, 35, 36})
	if err != nil {
		t.Fatalf("FetchTypeNames() error = %v", err)
	}
	if len(got) != 2 || got[34] != "Tritanium" || got[35] != "Pyerite" {
		t.Fatalf("FetchTypeNames() = %+v, want the two seeded names", got)
	}
	if _, ok := got[36]; ok {
		t.Fatalf("FetchTypeNames() resolved unseeded id 36")
	}
}

func TestFakeFetchTypeNamesReturnsSeededError(t *testing.T) {
	wantErr := errors.New("esi down")
	fake := &esi.Fake{FetchTypeNamesErr: wantErr}

	_, err := fake.FetchTypeNames(t.Context(), []int{34})
	if !errors.Is(err, wantErr) {
		t.Fatalf("FetchTypeNames() error = %v, want %v", err, wantErr)
	}
}

func TestFakeFetchCharacterSkillsReturnsSeededSkills(t *testing.T) {
	want := esi.Skills{BrokerRelationsLevel: 4, AccountingLevel: 5}
	fake := &esi.Fake{Skills: want}

	got, err := fake.FetchCharacterSkills(t.Context(), 12345, "access-token")
	if err != nil {
		t.Fatalf("FetchCharacterSkills() error = %v", err)
	}
	if got != want {
		t.Fatalf("FetchCharacterSkills() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchCharacterContractsReturnsSeededContracts(t *testing.T) {
	want := []esi.Contract{{ContractID: 1000, Type: "item_exchange", Price: 0}}
	fake := &esi.Fake{Contracts: want}

	got, err := fake.FetchCharacterContracts(t.Context(), 123, "access")
	if err != nil {
		t.Fatalf("FetchCharacterContracts() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FetchCharacterContracts() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchContractItemsReturnsSeededItemsByContractID(t *testing.T) {
	items := []esi.ContractItem{{RecordID: 1, TypeID: 34, Quantity: 500, IsIncluded: true}}
	fake := &esi.Fake{ContractItems: map[int64][]esi.ContractItem{1000: items}}

	got, err := fake.FetchContractItems(t.Context(), 123, "access", 1000)
	if err != nil {
		t.Fatalf("FetchContractItems() error = %v", err)
	}
	if len(got) != 1 || got[0] != items[0] {
		t.Fatalf("FetchContractItems() = %+v, want %+v", got, items)
	}

	empty, err := fake.FetchContractItems(t.Context(), 123, "access", 2000)
	if err != nil {
		t.Fatalf("FetchContractItems() error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("FetchContractItems() for unseeded contract = %+v, want empty", empty)
	}
}

func TestFakeExchangeCodeReturnsSeededToken(t *testing.T) {
	want := esi.Token{AccessToken: "access", RefreshToken: "refresh", CharacterID: 12345}
	fake := &esi.Fake{ExchangeCodeToken: want}

	got, err := fake.ExchangeCode(t.Context(), "code", "verifier")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if got != want {
		t.Fatalf("ExchangeCode() = %+v, want %+v", got, want)
	}
}

func TestFakeExchangeCodeReturnsSeededError(t *testing.T) {
	wantErr := errors.New("invalid code")
	fake := &esi.Fake{ExchangeCodeErr: wantErr}

	_, err := fake.ExchangeCode(t.Context(), "code", "verifier")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExchangeCode() error = %v, want %v", err, wantErr)
	}
}

func TestFakeRefreshTokenReturnsSeededToken(t *testing.T) {
	want := esi.Token{AccessToken: "access2", RefreshToken: "refresh2", CharacterID: 12345}
	fake := &esi.Fake{RefreshTokenToken: want}

	got, err := fake.RefreshToken(t.Context(), "refresh")
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if got != want {
		t.Fatalf("RefreshToken() = %+v, want %+v", got, want)
	}
}

func TestFakeRefreshTokenReturnsSeededError(t *testing.T) {
	wantErr := errors.New("token revoked")
	fake := &esi.Fake{RefreshTokenErr: wantErr}

	_, err := fake.RefreshToken(t.Context(), "refresh")
	if !errors.Is(err, wantErr) {
		t.Fatalf("RefreshToken() error = %v, want %v", err, wantErr)
	}
}

func TestFakeFetchCharacterOrdersReturnsSeededOrders(t *testing.T) {
	want := []esi.CharacterOrder{{OrderID: 1, TypeID: 34, LocationID: 60004588, IsBuyOrder: true, Price: 5.5}}
	fake := &esi.Fake{CharacterOrders: want}

	got, err := fake.FetchCharacterOrders(t.Context(), 12345, "access-token")
	if err != nil {
		t.Fatalf("FetchCharacterOrders() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FetchCharacterOrders() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchCharacterOrdersReturnsSeededError(t *testing.T) {
	wantErr := errors.New("esi down")
	fake := &esi.Fake{FetchCharacterOrdersErr: wantErr}

	_, err := fake.FetchCharacterOrders(t.Context(), 12345, "access-token")
	if !errors.Is(err, wantErr) {
		t.Fatalf("FetchCharacterOrders() error = %v, want %v", err, wantErr)
	}
}

func TestFakeFetchCharacterOrderHistoryReturnsSeededOrders(t *testing.T) {
	want := []esi.CharacterOrder{{OrderID: 2, TypeID: 626, LocationID: 60004588, State: "cancelled"}}
	fake := &esi.Fake{CharacterOrderHistory: want}

	got, err := fake.FetchCharacterOrderHistory(t.Context(), 12345, "access-token")
	if err != nil {
		t.Fatalf("FetchCharacterOrderHistory() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FetchCharacterOrderHistory() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchCharacterOrderHistoryReturnsSeededError(t *testing.T) {
	wantErr := errors.New("esi down")
	fake := &esi.Fake{FetchCharacterOrderHistoryErr: wantErr}

	_, err := fake.FetchCharacterOrderHistory(t.Context(), 12345, "access-token")
	if !errors.Is(err, wantErr) {
		t.Fatalf("FetchCharacterOrderHistory() error = %v, want %v", err, wantErr)
	}
}

var _ esi.ESIGateway = (*esi.Fake)(nil)
