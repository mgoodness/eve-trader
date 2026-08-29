package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// fakeCharacterAccessToken builds a JWT-shaped string whose `sub` claim
// encodes characterID, the format auth.CharacterIDFromAccessToken expects.
func fakeCharacterAccessToken(t *testing.T, characterID int32) string {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"sub": fmt.Sprintf("CHARACTER:EVE:%d", characterID)})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

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

	return NewServer(esiClient, store, noopNotifier(), testClientID, testCallbackURL)
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

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want %q", loc, "/")
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

	if code := <-done; code != http.StatusFound {
		t.Fatalf("first callback status = %d, want %d", code, http.StatusFound)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code-2&state="+state2, nil)
	srv.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusFound {
		t.Fatalf("second callback status = %d, want %d (state2 must survive the first callback's completion)", rec2.Code, http.StatusFound)
	}
}

// authenticate drives a full login -> callback round trip against srv so a
// test can exercise handlers that require a stored token.
func authenticate(t *testing.T, srv *Server) {
	t.Helper()

	state := loginState(t, srv)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=auth-code&state="+state, nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d, body = %s", rec.Code, http.StatusFound, rec.Body.String())
	}
}

func TestDashboardShowsLoginPromptWhenNotAuthenticated(t *testing.T) {
	srv := newTestServer(t, esi.NewFakeClient())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `href="/auth/login"`) {
		t.Errorf("body missing login link:\n%s", rec.Body.String())
	}
}

// TestDashboardRendersOrdersFromFakeESIClient is the AC-mandated test: a
// handler test, driven by a fake ESIClient, proving the Orders list
// renders correctly from canned data.
func TestDashboardRendersOrdersFromFakeESIClient(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.TokenResp.AccessToken = fakeCharacterAccessToken(t, 95465499)
	srv := newTestServer(t, fake)

	authenticate(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"Tritanium", // resolved item name for fake.Orders[0].TypeID == 34
		"Jita",      // hub for LocationID 60003760
		"Sell",      // fake.Orders[0].IsBuyOrder == false
		"8,000",     // VolumeRemain
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard body missing %q:\n%s", want, body)
		}
	}
}

func TestDashboardPropagatesESIErrorAsBadGateway(t *testing.T) {
	fake := esi.NewFakeClient()
	fake.TokenResp.AccessToken = fakeCharacterAccessToken(t, 95465499)
	fake.OrdersErr = context.DeadlineExceeded
	srv := newTestServer(t, fake)

	authenticate(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
