package esi_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

func TestFakeFetchRensOrdersReturnsSeededOrders(t *testing.T) {
	want := []esi.Order{
		{OrderID: 1, TypeID: 34, IsBuyOrder: false, Price: 5.5, VolumeRemain: 100},
	}
	fake := &esi.Fake{Orders: want}

	got, err := fake.FetchRensOrders(t.Context())
	if err != nil {
		t.Fatalf("FetchRensOrders() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FetchRensOrders() = %+v, want %+v", got, want)
	}
}

func TestFakeFetchRensOrdersReturnsSeededError(t *testing.T) {
	wantErr := errors.New("esi down")
	fake := &esi.Fake{FetchOrdersErr: wantErr}

	_, err := fake.FetchRensOrders(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("FetchRensOrders() error = %v, want %v", err, wantErr)
	}
}

func TestFakeFetchHistoryReturnsSeededPointsByTypeID(t *testing.T) {
	pts := []esi.HistoryPoint{{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Volume: 42, OrderCount: 3}}
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

var _ esi.ESIGateway = (*esi.Fake)(nil)
