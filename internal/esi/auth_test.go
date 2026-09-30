package esi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
