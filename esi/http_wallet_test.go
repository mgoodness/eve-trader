package esi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchWalletTransactionsSendsFromIDAndAuth(t *testing.T) {
	var gotAuth, gotFromID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/wallet/transactions/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotFromID = r.URL.Query().Get("from_id")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"transaction_id": 100, "date": "2026-09-16T00:00:00Z", "type_id": 34,
				"quantity": 10, "unit_price": 5.5, "is_buy": true, "is_personal": true,
				"journal_ref_id": 200, "location_id": 60004588, "client_id": 999,
			},
		})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchWalletTransactions(context.Background(), 123, "access", 500)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer access" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if gotFromID != "500" {
		t.Fatalf("from_id query = %q, want 500", gotFromID)
	}
	if len(got) != 1 {
		t.Fatalf("transactions = %+v, want one", got)
	}
	tx := got[0]
	if tx.TransactionID != 100 || tx.TypeID != 34 || tx.Quantity != 10 || tx.UnitPrice != 5.5 ||
		!tx.IsBuy || !tx.IsPersonal || tx.JournalRefID != 200 || tx.LocationID != 60004588 || tx.ClientID != 999 {
		t.Fatalf("transaction = %+v", tx)
	}
}

func TestHTTPGatewayFetchWalletTransactionsOmitsFromIDWhenZero(t *testing.T) {
	var sawFromID bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawFromID = r.URL.Query().Has("from_id")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer server.Close()

	if _, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchWalletTransactions(context.Background(), 123, "access", 0); err != nil {
		t.Fatal(err)
	}
	if sawFromID {
		t.Fatal("from_id present in query with fromID=0, want it omitted")
	}
}

func TestHTTPGatewayFetchWalletJournalPaginatesAndLinksContextID(t *testing.T) {
	pages := map[string][]map[string]any{
		"1": {{"id": 1, "date": "2026-09-16T00:00:00Z", "ref_type": "market_transaction", "amount": 55.0, "balance": 1000.0, "context_id": 100, "context_id_type": "market_transaction_id", "description": "sale"}},
		"2": {{"id": 2, "date": "2026-09-16T00:00:00Z", "ref_type": "brokers_fee", "amount": -5.0, "balance": 995.0, "description": "broker fee"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/wallet/journal/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("Authorization = %q", got)
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("X-Pages", "2")
		if err := json.NewEncoder(w).Encode(pages[page]); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchWalletJournal(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("journal entries = %+v, want 2 across both pages", got)
	}
	if got[0].ID != 1 || got[0].ContextID != 100 || got[0].ContextIDType != "market_transaction_id" {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].ID != 2 || got[1].ContextID != 0 {
		t.Fatalf("entry 1 = %+v, want no context_id on a non-transaction ref_type", got[1])
	}
}
