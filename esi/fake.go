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

	TypeNames         map[int]string
	FetchTypeNamesErr error

	History         map[int][]HistoryPoint
	FetchHistoryErr error

	Skills         Skills
	FetchSkillsErr error

	WalletTransactions         []WalletTransaction
	FetchWalletTransactionsErr error

	WalletJournal         []WalletJournalEntry
	FetchWalletJournalErr error

	Contracts                  []Contract
	FetchCharacterContractsErr error

	ContractItems         map[int64][]ContractItem
	FetchContractItemsErr error

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

// FetchTypeNames returns the seeded TypeNames for the requested IDs, or
// FetchTypeNamesErr if set. IDs missing from TypeNames are omitted.
func (f *Fake) FetchTypeNames(ctx context.Context, typeIDs []int) (map[int]string, error) {
	if f.FetchTypeNamesErr != nil {
		return nil, f.FetchTypeNamesErr
	}
	names := make(map[int]string, len(typeIDs))
	for _, id := range typeIDs {
		if name, ok := f.TypeNames[id]; ok {
			names[id] = name
		}
	}
	return names, nil
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

// FetchWalletTransactions returns the seeded WalletTransactions, or
// FetchWalletTransactionsErr if set. It mimics ESI's inclusive from_id
// boundary: a non-zero fromID returns only the seeded transactions with
// TransactionID <= fromID (including fromID's own transaction again, if
// seeded), so callers walking backward exercise the same duplicate-drop
// logic they need against the real API. fromID of zero returns every
// seeded transaction.
func (f *Fake) FetchWalletTransactions(ctx context.Context, characterID int, accessToken string, fromID int64) ([]WalletTransaction, error) {
	if f.FetchWalletTransactionsErr != nil {
		return nil, f.FetchWalletTransactionsErr
	}
	if fromID == 0 {
		return f.WalletTransactions, nil
	}
	var out []WalletTransaction
	for _, tx := range f.WalletTransactions {
		if tx.TransactionID <= fromID {
			out = append(out, tx)
		}
	}
	return out, nil
}

// FetchWalletJournal returns the seeded WalletJournal, or
// FetchWalletJournalErr if set.
func (f *Fake) FetchWalletJournal(ctx context.Context, characterID int, accessToken string) ([]WalletJournalEntry, error) {
	if f.FetchWalletJournalErr != nil {
		return nil, f.FetchWalletJournalErr
	}
	return f.WalletJournal, nil
}

// FetchCharacterContracts returns the seeded Contracts, or
// FetchCharacterContractsErr if set.
func (f *Fake) FetchCharacterContracts(ctx context.Context, characterID int, accessToken string) ([]Contract, error) {
	if f.FetchCharacterContractsErr != nil {
		return nil, f.FetchCharacterContractsErr
	}
	return f.Contracts, nil
}

// FetchContractItems returns the seeded items for contractID, or
// FetchContractItemsErr if set. An unseeded contract ID returns an empty
// slice, not an error.
func (f *Fake) FetchContractItems(ctx context.Context, characterID int, accessToken string, contractID int64) ([]ContractItem, error) {
	if f.FetchContractItemsErr != nil {
		return nil, f.FetchContractItemsErr
	}
	return f.ContractItems[contractID], nil
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
