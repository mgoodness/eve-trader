package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

func newTestServer(t *testing.T, esiClient esi.Client) *Server {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	return NewServer(esiClient, store)
}

func TestHealthCheck(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestCharacterOrders drives the full handler stack -- HTTP routing, the
// fake ESIClient, and a real temp-file SQLite store -- to prove the
// ESIClient seam works end-to-end.
func TestCharacterOrders(t *testing.T) {
	fake := esi.NewFakeClient()
	srv := newTestServer(t, fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/characters/12345/orders", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []esi.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != len(fake.Orders) {
		t.Fatalf("len(orders) = %d, want %d", len(got), len(fake.Orders))
	}
	if got[0].OrderID != fake.Orders[0].OrderID {
		t.Errorf("orders[0].OrderID = %d, want %d", got[0].OrderID, fake.Orders[0].OrderID)
	}
	if got[0].TypeID != fake.Orders[0].TypeID {
		t.Errorf("orders[0].TypeID = %d, want %d", got[0].TypeID, fake.Orders[0].TypeID)
	}
}

func TestCharacterOrdersInvalidID(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/characters/not-a-number/orders", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
