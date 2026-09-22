package esi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchCharacterContractsPaginatesAndParsesFields(t *testing.T) {
	pages := map[string][]map[string]any{
		"1": {{
			"contract_id": 1000, "issuer_id": 123, "issuer_corporation_id": 456,
			"assignee_id": 789, "acceptor_id": 0, "type": "item_exchange",
			"status": "finished", "price": 0.0, "for_corporation": false,
			"date_issued": "2026-09-16T00:00:00Z", "date_expired": "2026-09-23T00:00:00Z",
			"date_completed": "2026-09-17T00:00:00Z", "title": "handoff",
		}},
		"2": {{
			"contract_id": 1001, "issuer_id": 999, "type": "courier", "status": "outstanding",
			"start_location_id": 60004588, "end_location_id": 60004589,
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/contracts/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("X-Pages", "2")
		if err := json.NewEncoder(w).Encode(pages[r.URL.Query().Get("page")]); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchCharacterContracts(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("contracts = %+v, want 2 across both pages", got)
	}
	c := got[0]
	if c.ContractID != 1000 || c.IssuerID != 123 || c.IssuerCorporationID != 456 || c.AssigneeID != 789 ||
		c.Type != "item_exchange" || c.Status != "finished" || c.Price != 0 {
		t.Fatalf("contract 0 = %+v", c)
	}
	if got[1].StartLocationID != 60004588 || got[1].EndLocationID != 60004589 {
		t.Fatalf("contract 1 = %+v, want courier locations", got[1])
	}
}

func TestHTTPGatewayFetchContractItemsParsesIncludedAndSingleton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/contracts/1000/items/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"record_id": 1, "type_id": 34, "quantity": 500, "is_singleton": false, "is_included": true},
		})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchContractItems(context.Background(), 123, "access", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("items = %+v, want one", got)
	}
	item := got[0]
	if item.RecordID != 1 || item.TypeID != 34 || item.Quantity != 500 || item.IsSingleton || !item.IsIncluded {
		t.Fatalf("item = %+v", item)
	}
}
