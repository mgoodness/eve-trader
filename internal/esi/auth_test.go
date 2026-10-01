package esi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
)

// jwtWithSub builds an unsigned JWT-shaped string whose payload has the
// given sub claim (docs/research/esi-sso-cli.md §4.3: the character id is
// the third field of "CHARACTER:EVE:<character-id>"). The header and
// signature segments are irrelevant to CharacterIDFromAccessToken, which
// only decodes the payload.
func jwtWithSub(t *testing.T, sub string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"sub": sub})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestCharacterIDFromAccessTokenReadsTheSubClaim(t *testing.T) {
	token := jwtWithSub(t, "CHARACTER:EVE:932683762")

	got, err := esi.CharacterIDFromAccessToken(token)
	if err != nil {
		t.Fatalf("CharacterIDFromAccessToken: %v", err)
	}
	if got != 932683762 {
		t.Errorf("got character id %d, want 932683762", got)
	}
}

func TestCharacterIDFromAccessTokenRejectsATokenThatIsNotThreeSegments(t *testing.T) {
	if _, err := esi.CharacterIDFromAccessToken("not-a-jwt"); err == nil {
		t.Fatal("got no error for a malformed token, want one")
	}
}

func TestCharacterIDFromAccessTokenRejectsASubClaimThatIsNotACharacterID(t *testing.T) {
	token := jwtWithSub(t, "not-the-right-shape")

	if _, err := esi.CharacterIDFromAccessToken(token); err == nil {
		t.Fatal("got no error for a malformed sub claim, want one")
	}
}

func TestRefreshAccessTokenPostsTheRefreshGrantAndReturnsTheNewTokens(t *testing.T) {
	var gotBody, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotBody = r.Form.Encode()
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  jwtWithSub(t, "CHARACTER:EVE:932683762"),
			"token_type":    "Bearer",
			"expires_in":    1200,
			"refresh_token": "rotated-refresh-token",
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{SSOBaseURL: server.URL})

	token, err := client.RefreshAccessToken(t.Context(), "client-id", "old-refresh-token")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}

	if token.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want the rotated one", token.RefreshToken)
	}
	if token.AccessToken == "" {
		t.Error("got empty access token")
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("got Content-Type %q, want application/x-www-form-urlencoded", gotContentType)
	}
	for _, want := range []string{"grant_type=refresh_token", "refresh_token=old-refresh-token", "client_id=client-id"} {
		if !containsParam(gotBody, want) {
			t.Errorf("request body %q missing %q", gotBody, want)
		}
	}
}

func TestRefreshAccessTokenSurfacesANonOKStatusAsAClearError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{SSOBaseURL: server.URL})

	_, err := client.RefreshAccessToken(t.Context(), "client-id", "old-refresh-token")
	if err == nil {
		t.Fatal("got no error for a non-200 response, want one")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("got error %q, want it to mention the status and body", err.Error())
	}
}

// TestPKCEChallengeMatchesEVESSOsNonRFC7636Vector asserts
// esi.PKCEChallenge against a fixed, independently computed vector
// (docs/research/esi-sso-cli.md §1.3: challenge = base64url(sha256(ASCII(verifier
// string))), unpadded — not the RFC 7636 default of hashing the raw
// verifier bytes). The expected value here was computed independently
// with CCP's own Python snippet, not by re-deriving it the way the code
// does.
func TestPKCEChallengeMatchesEVESSOsNonRFC7636Vector(t *testing.T) {
	const verifier = "dGhpcy1pcy1hLWZpeGVkLXRlc3QtdmVyaWZpZXItZm9yLXBrY2U="
	const wantChallenge = "OJ0biabRGoE82fnt3yguNVB9JG6N4vTUyfAz69HKyNs"

	if got := esi.PKCEChallenge(verifier); got != wantChallenge {
		t.Errorf("PKCEChallenge(%q) = %q, want %q", verifier, got, wantChallenge)
	}
}

// TestAuthorizeURLSetsTheExactParametersEVESSORequires asserts the
// authorize URL's query parameters against the authorization request
// spec (docs/research/esi-sso-cli.md \u00a71.2): response_type=code, the fixed
// scope list, code_challenge_method=S256, and the caller-supplied state,
// client id, redirect URI, and challenge passed through unmodified.
// TestExchangeAuthorizationCodePostsFormEncodedWithNoAuthorizationHeader
// asserts the authorization-code exchange's request shape
// (docs/research/esi-sso-cli.md \u00a71.2, \u00a71.3: "No Authorization: Basic header
// in the PKCE flow"): form-encoded body with grant_type=authorization_code,
// code, code_verifier, and client_id, and no Authorization header (public
// PKCE client, no client secret).
func TestExchangeAuthorizationCodePostsFormEncodedWithNoAuthorizationHeader(t *testing.T) {
	var gotBody, gotContentType, gotAuthHeader string
	gotAuthHeaderSet := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotBody = r.Form.Encode()
		gotContentType = r.Header.Get("Content-Type")
		if auth := r.Header.Get("Authorization"); auth != "" {
			gotAuthHeaderSet = true
			gotAuthHeader = auth
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  jwtWithSub(t, "CHARACTER:EVE:932683762"),
			"token_type":    "Bearer",
			"expires_in":    1200,
			"refresh_token": "rotated-refresh-token",
		})
	}))
	defer server.Close()

	client := esi.NewClient(esi.ClientOptions{SSOBaseURL: server.URL})

	token, err := client.ExchangeAuthorizationCode(t.Context(), "client-id", "http://127.0.0.1:8000/callback", "the-auth-code", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}

	if token.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want the rotated one", token.RefreshToken)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("got Content-Type %q, want application/x-www-form-urlencoded", gotContentType)
	}
	if gotAuthHeaderSet {
		t.Errorf("got Authorization header %q, want none (public PKCE client, no client secret)", gotAuthHeader)
	}
	for _, want := range []string{"grant_type=authorization_code", "code=the-auth-code", "code_verifier=the-verifier", "client_id=client-id"} {
		if !containsParam(gotBody, want) {
			t.Errorf("request body %q missing %q", gotBody, want)
		}
	}
}

func TestAuthorizeURLSetsTheExactParametersEVESSORequires(t *testing.T) {
	got := esi.AuthorizeURL("https://login.eveonline.com", "my-client-id", "http://127.0.0.1:8000/callback", "my-state", "my-challenge")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("authorize URL %q does not parse: %v", got, err)
	}
	if gotURL := u.Scheme + "://" + u.Host + u.Path; gotURL != "https://login.eveonline.com/v2/oauth/authorize" {
		t.Errorf("got base URL %q, want https://login.eveonline.com/v2/oauth/authorize", gotURL)
	}

	q := u.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             "my-client-id",
		"redirect_uri":          "http://127.0.0.1:8000/callback",
		"scope":                 esi.Scopes,
		"state":                 "my-state",
		"code_challenge":        "my-challenge",
		"code_challenge_method": "S256",
	}
	for key, wantVal := range want {
		if got := q.Get(key); got != wantVal {
			t.Errorf("query param %q = %q, want %q", key, got, wantVal)
		}
	}
}

// TestAuthorizeURLDefaultsAnEmptySSOBaseURL is a regression test (diagnosed
// bug: cli.DefaultConfig leaves SSOBaseURL at its zero value, and
// internal/cli/login.go calls AuthorizeURL directly rather than through a
// Client, so a production run produced a relative, unopenable consent URL
// -- "/v2/oauth/authorize?..." with no scheme or host). AuthorizeURL must
// default an empty ssoBaseURL exactly like NewClient does.
func TestAuthorizeURLDefaultsAnEmptySSOBaseURL(t *testing.T) {
	got := esi.AuthorizeURL("", "my-client-id", "http://127.0.0.1:8000/callback", "my-state", "my-challenge")

	if !strings.HasPrefix(got, "https://login.eveonline.com/v2/oauth/authorize?") {
		t.Fatalf("AuthorizeURL with empty ssoBaseURL = %q, want it to default to the production SSO host", got)
	}
}

func containsParam(form, wantPair string) bool {
	for _, pair := range splitAmp(form) {
		if pair == wantPair {
			return true
		}
	}
	return false
}

func splitAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
