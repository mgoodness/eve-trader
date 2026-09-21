package server_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

// noRedirectClient returns an *http.Client that carries cookies (needed
// to round-trip the PKCE cookie between /auth/login and /auth/callback)
// but never follows a redirect -- callers need the redirect Location and
// Set-Cookie headers rather than the target page.
func noRedirectClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// TestAuthLoginRedirectsToEVESSO asserts /auth/login redirects to EVE
// SSO's authorization endpoint requesting the v2 scope set (v1's
// character-skills scope plus wallet/orders/contracts), with a PKCE S256
// challenge, and sets the short-lived PKCE cookie.
func TestAuthLoginRedirectsToEVESSO(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	client := noRedirectClient(t)
	resp, err := client.Get(srv.URL + "/auth/login")
	if err != nil {
		t.Fatalf("GET /auth/login error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /auth/login status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location header: %v", err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != "https://login.eveonline.com/v2/oauth/authorize" {
		t.Errorf("Location authorize endpoint = %q, want EVE SSO's authorization endpoint", got)
	}

	q := loc.Query()
	for key, want := range map[string]string{
		"response_type":         "code",
		"client_id":             "test-client-id",
		"redirect_uri":          "http://example.com/auth/callback",
		"scope":                 "esi-skills.read_skills.v1 esi-wallet.read_character_wallet.v1 esi-markets.read_character_orders.v1 esi-contracts.read_character_contracts.v1",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("Location query %q = %q, want %q", key, got, want)
		}
	}
	if q.Get("state") == "" {
		t.Error("Location query \"state\" is empty, want a PKCE state value")
	}
	if q.Get("code_challenge") == "" {
		t.Error("Location query \"code_challenge\" is empty, want a PKCE S256 challenge")
	}

	pkceCookie := findPKCECookie(t, resp.Cookies())
	if !pkceCookie.HttpOnly {
		t.Error("eve_trader_pkce cookie is not HttpOnly")
	}

	// code_challenge must be the S256 (base64url(SHA-256)) of the
	// verifier held in the signed cookie (RFC 7636).
	cookiePayload := decodePKCECookiePayload(t, pkceCookie.Value)
	sum := sha256.Sum256([]byte(cookiePayload.Verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := q.Get("code_challenge"); got != wantChallenge {
		t.Errorf("Location query \"code_challenge\" = %q, want S256(verifier) = %q", got, wantChallenge)
	}
}

// TestAuthCallbackPersistsTokenAndSkills is the ticket's headline
// acceptance test: it drives /auth/login -> /auth/callback against a
// fake ESIGateway, then asserts the encrypted refresh token and the
// character's fetched skills were persisted to SQLite.
func TestAuthCallbackPersistsTokenAndSkills(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{
		ExchangeCodeToken: esi.Token{
			AccessToken:  "access-tok",
			RefreshToken: "refresh-tok-secret",
			CharacterID:  12345,
			OwnerHash:    "owner-hash-abc",
		},
		Skills: esi.Skills{BrokerRelationsLevel: 4, AccountingLevel: 3},
	}
	srv := httptest.NewServer(server.New(fake, sqlDB, testAuthConfig()))
	defer srv.Close()

	client := noRedirectClient(t)

	loginResp, err := client.Get(srv.URL + "/auth/login")
	if err != nil {
		t.Fatalf("GET /auth/login error = %v", err)
	}
	loc, err := url.Parse(loginResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location header: %v", err)
	}
	loginResp.Body.Close()
	state := loc.Query().Get("state")

	callbackResp, err := client.Get(srv.URL + "/auth/callback?code=test-code&state=" + state)
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer callbackResp.Body.Close()
	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("GET /auth/callback status = %d, want %d", callbackResp.StatusCode, http.StatusFound)
	}

	var characterID int
	var ownerHash string
	var encrypted []byte
	if err := sqlDB.QueryRow(
		`SELECT character_id, owner_hash, encrypted_refresh_token FROM esi_token`,
	).Scan(&characterID, &ownerHash, &encrypted); err != nil {
		t.Fatalf("querying esi_token: %v", err)
	}
	if characterID != 12345 {
		t.Errorf("esi_token.character_id = %d, want 12345", characterID)
	}
	if ownerHash != "owner-hash-abc" {
		t.Errorf("esi_token.owner_hash = %q, want %q", ownerHash, "owner-hash-abc")
	}
	if len(encrypted) == 0 {
		t.Error("esi_token.encrypted_refresh_token is empty")
	}
	if strings.Contains(string(encrypted), "refresh-tok-secret") {
		t.Error("esi_token.encrypted_refresh_token contains the plaintext refresh token, want it encrypted")
	}

	var broker, accounting int
	if err := sqlDB.QueryRow(
		`SELECT broker_relations_level, accounting_level FROM character_skill WHERE character_id = ?`, characterID,
	).Scan(&broker, &accounting); err != nil {
		t.Fatalf("querying character_skill: %v", err)
	}
	if broker != 4 {
		t.Errorf("character_skill.broker_relations_level = %d, want 4", broker)
	}
	if accounting != 3 {
		t.Errorf("character_skill.accounting_level = %d, want 3", accounting)
	}
}

// TestAuthCallbackRejectsMissingPKCECookie asserts hitting
// /auth/callback without first visiting /auth/login (so no PKCE cookie
// is set) fails, and persists nothing.
func TestAuthCallbackRejectsMissingPKCECookie(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/auth/callback?code=test-code&state=anything")
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /auth/callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM esi_token`).Scan(&count); err != nil {
		t.Fatalf("querying esi_token: %v", err)
	}
	if count != 0 {
		t.Errorf("esi_token row count = %d, want 0", count)
	}
}

// TestAuthCallbackRejectsStateMismatch asserts a state parameter that
// doesn't match the PKCE cookie's state is rejected (CSRF protection).
func TestAuthCallbackRejectsStateMismatch(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	client := noRedirectClient(t)
	loginResp, err := client.Get(srv.URL + "/auth/login")
	if err != nil {
		t.Fatalf("GET /auth/login error = %v", err)
	}
	loginResp.Body.Close()

	resp, err := client.Get(srv.URL + "/auth/callback?code=test-code&state=wrong-state")
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /auth/callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestAuthCallbackHandlesExchangeCodeError asserts an ESIGateway.
// ExchangeCode failure surfaces as an error response and persists
// nothing.
func TestAuthCallbackHandlesExchangeCodeError(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{ExchangeCodeErr: errors.New("invalid code")}
	srv := httptest.NewServer(server.New(fake, sqlDB, testAuthConfig()))
	defer srv.Close()

	client := noRedirectClient(t)
	loginResp, err := client.Get(srv.URL + "/auth/login")
	if err != nil {
		t.Fatalf("GET /auth/login error = %v", err)
	}
	loc, err := url.Parse(loginResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location header: %v", err)
	}
	loginResp.Body.Close()
	state := loc.Query().Get("state")

	resp, err := client.Get(srv.URL + "/auth/callback?code=test-code&state=" + state)
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("GET /auth/callback status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM esi_token`).Scan(&count); err != nil {
		t.Fatalf("querying esi_token: %v", err)
	}
	if count != 0 {
		t.Errorf("esi_token row count = %d, want 0", count)
	}
}

// TestAuthCallbackRejectsTamperedPKCECookie asserts a PKCE cookie signed
// with a secret other than the server's AuthConfig.CookieSecret (i.e. a
// forged/tampered one) is rejected.
func TestAuthCallbackRejectsTamperedPKCECookie(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, testAuthConfig()))
	defer srv.Close()

	forged := signPKCECookie(t, "not-the-servers-cookie-secret", pkceCookiePayload{
		State:    "forged-state",
		Verifier: "forged-verifier",
		Expires:  time.Now().Add(5 * time.Minute).Unix(),
	})

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/auth/callback?code=test-code&state=forged-state", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "eve_trader_pkce", Value: forged})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /auth/callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestAuthCallbackRejectsExpiredPKCECookie asserts a validly-signed but
// expired PKCE cookie is rejected -- the cookie is short-lived by design
// (docs/spec/v1.md §6).
func TestAuthCallbackRejectsExpiredPKCECookie(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	cfg := testAuthConfig()
	srv := httptest.NewServer(server.New(&esi.Fake{}, sqlDB, cfg))
	defer srv.Close()

	expired := signPKCECookie(t, cfg.CookieSecret, pkceCookiePayload{
		State:    "expired-state",
		Verifier: "expired-verifier",
		Expires:  time.Now().Add(-time.Minute).Unix(),
	})

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/auth/callback?code=test-code&state=expired-state", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "eve_trader_pkce", Value: expired})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /auth/callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /auth/callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// pkceCookiePayload mirrors the unexported pkceState JSON shape package
// server signs into the PKCE cookie. Test code reconstructs
// (signPKCECookie) and deconstructs (decodePKCECookiePayload) cookie
// values using only this package's own crypto primitives against the
// server's public HTTP surface -- never reaching into package server's
// unexported internals -- matching this repo's black-box-only test
// convention.
type pkceCookiePayload struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"expires"`
}

// signPKCECookie replicates package server's PKCE cookie encoding
// (base64url(json) + "." + base64url(HMAC-SHA256(json))) so tests can
// construct cookies exercising cases (tampering, expiry) that the real
// /auth/login handler would never itself produce.
func signPKCECookie(t *testing.T, secret string, p pkceCookiePayload) string {
	t.Helper()
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshaling PKCE cookie payload: %v", err)
	}
	key := sha256.Sum256([]byte(secret))
	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// decodePKCECookiePayload decodes (without verifying the signature) the
// payload half of a PKCE cookie value.
func decodePKCECookiePayload(t *testing.T, cookieValue string) pkceCookiePayload {
	t.Helper()
	payloadPart, _, ok := strings.Cut(cookieValue, ".")
	if !ok {
		t.Fatalf("malformed PKCE cookie value %q", cookieValue)
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		t.Fatalf("decoding PKCE cookie payload: %v", err)
	}
	var p pkceCookiePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("unmarshaling PKCE cookie payload: %v", err)
	}
	return p
}

// findPKCECookie returns the eve_trader_pkce cookie from cookies, failing
// the test if it's absent.
func findPKCECookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == "eve_trader_pkce" {
			return c
		}
	}
	t.Fatal("response missing eve_trader_pkce cookie")
	return nil
}
