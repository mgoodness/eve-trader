package esi

import "context"

// Fake must keep satisfying ESIGateway at compile time.
var _ ESIGateway = (*Fake)(nil)

// Fake is an in-memory ESIGateway implementation for tests. Zero value is
// usable: seed the fields it needs to serve, or set the *Err fields to
// make a method fail.
type Fake struct {
	Orders         []Order
	FetchOrdersErr error

	History         map[int][]HistoryPoint
	FetchHistoryErr error

	Skills         Skills
	FetchSkillsErr error

	ExchangeCodeToken Token
	ExchangeCodeErr   error

	RefreshTokenToken Token
	RefreshTokenErr   error
}

// FetchRensOrders returns the seeded Orders, or FetchOrdersErr if set.
func (f *Fake) FetchRensOrders(ctx context.Context) ([]Order, error) {
	if f.FetchOrdersErr != nil {
		return nil, f.FetchOrdersErr
	}
	return f.Orders, nil
}

// FetchHistory returns the seeded History for typeID, or FetchHistoryErr
// if set. An unseeded typeID returns an empty slice, not an error.
func (f *Fake) FetchHistory(ctx context.Context, typeID int) ([]HistoryPoint, error) {
	if f.FetchHistoryErr != nil {
		return nil, f.FetchHistoryErr
	}
	return f.History[typeID], nil
}

// FetchCharacterSkills returns the seeded Skills, or FetchSkillsErr if set.
func (f *Fake) FetchCharacterSkills(ctx context.Context, characterID int, accessToken string) (Skills, error) {
	if f.FetchSkillsErr != nil {
		return Skills{}, f.FetchSkillsErr
	}
	return f.Skills, nil
}

// ExchangeCode returns the seeded ExchangeCodeToken, or ExchangeCodeErr if
// set.
func (f *Fake) ExchangeCode(ctx context.Context, code, verifier string) (Token, error) {
	if f.ExchangeCodeErr != nil {
		return Token{}, f.ExchangeCodeErr
	}
	return f.ExchangeCodeToken, nil
}

// RefreshToken returns the seeded RefreshTokenToken, or RefreshTokenErr if
// set.
func (f *Fake) RefreshToken(ctx context.Context, refreshToken string) (Token, error) {
	if f.RefreshTokenErr != nil {
		return Token{}, f.RefreshTokenErr
	}
	return f.RefreshTokenToken, nil
}
