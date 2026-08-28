package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

const (
	testClientID    = "test-client-id"
	testCallbackURL = "https://eve-trader.opsgoodness.net/auth/callback"
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

	return NewServer(esiClient, store, testClientID, testCallbackURL)
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

func TestLoginRedirectsToAuthorizeURL(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location header: %v", err)
	}
	if loc.Scheme+"://"+loc.Host+loc.Path != "https://login.eveonline.com/v2/oauth/authorize" {
		t.Errorf("redirect target = %q, want the EVE SSO authorize endpoint", loc.Scheme+"://"+loc.Host+loc.Path)
	}

	q := loc.Query()
	if q.Get("client_id") != testClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), testClientID)
	}
	if q.Get("redirect_uri") != testCallbackURL {
		t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), testCallbackURL)
	}
	if q.Get("state") == "" {
		t.Error("state is empty, want a CSRF-protection value")
	}
}

// loginState performs the login redirect and extracts the state value the
// server generated, for use in a subsequent callback request.
func loginState(t *testing.T, srv *Server) string {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	srv.ServeHTTP(rec, req)

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location header: %v", err)
	}
	return loc.Query().Get("state")
}

func TestAuthCallbackCompletesExchangeAndPersistsToken(t *testing.T) {
	fake := esi.NewFakeClient()
	srv := newTestServer(t, fake)

	state := loginState(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state="+state, nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got, err := srv.store.LoadToken(context.Background())
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.RefreshToken != fake.TokenResp.RefreshToken {
		t.Errorf("persisted RefreshToken = %q, want %q", got.RefreshToken, fake.TokenResp.RefreshToken)
	}
}

func TestAuthCallbackRejectsMismatchedState(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())
	loginState(t, srv) // establishes a pending state, but we don't use it below

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state=wrong-state", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuthCallbackMissingCode(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())
	state := loginState(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state, nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// blockingExchangeClient wraps a FakeClient so a test can control exactly
// when ExchangeCode returns, to deterministically interleave a second
// /auth/login with an in-flight /auth/callback.
type blockingExchangeClient struct {
	*esi.FakeClient
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

// ExchangeCode blocks only on its first call -- the one the test wants to
// hold open -- and passes every subsequent call straight through.
func (c *blockingExchangeClient) ExchangeCode(ctx context.Context, code string) (esi.Token, error) {
	c.once.Do(func() {
		close(c.entered)
		<-c.proceed
	})
	return c.FakeClient.ExchangeCode(ctx, code)
}

// TestAuthCallbackDoesNotClearANewerPendingState guards against the race
// where /auth/callback unconditionally clears Server.pendingState after a
// slow exchange: if a second /auth/login started in the meantime, that
// unconditional clear would wipe out the second login's still-pending
// state before its own callback ever arrives.
func TestAuthCallbackDoesNotClearANewerPendingState(t *testing.T) {
	client := &blockingExchangeClient{
		FakeClient: esi.NewFakeClient(),
		entered:    make(chan struct{}),
		proceed:    make(chan struct{}),
	}
	srv := newTestServer(t, client)

	state1 := loginState(t, srv)

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code-1&state="+state1, nil)
		srv.ServeHTTP(rec, req)
		done <- rec.Code
	}()

	<-client.entered // callback for state1 has captured wantState and is now blocked in Exchange

	state2 := loginState(t, srv) // a second login overwrites pendingState

	close(client.proceed) // let the first callback's Exchange complete

	if code := <-done; code != http.StatusOK {
		t.Fatalf("first callback status = %d, want %d", code, http.StatusOK)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code-2&state="+state2, nil)
	srv.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second callback status = %d, want %d (state2 must survive the first callback's completion)", rec2.Code, http.StatusOK)
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
