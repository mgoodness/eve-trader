package esi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayFetchCharacterSkillsUsesActiveLevels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest/characters/123/skills/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Fatalf("authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"skills": []map[string]any{
			{"skill_id": 3446, "active_skill_level": 4, "trained_skill_level": 5},
			{"skill_id": 16622, "active_skill_level": 3},
		}})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{BaseURL: server.URL + "/latest"}).FetchCharacterSkills(context.Background(), 123, "access")
	if err != nil {
		t.Fatal(err)
	}
	if got != (esi.Skills{BrokerRelationsLevel: 4, AccountingLevel: 3}) {
		t.Fatalf("skills = %+v", got)
	}
}

func TestHTTPGatewayRefreshTokenParsesTokenClaims(t *testing.T) {
	accessToken := testJWT(t, map[string]string{"sub": "CHARACTER:EVE:987", "owner": "owner-hash"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("form = %v", r.Form)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": accessToken, "refresh_token": "new-refresh", "expires_in": 1200,
		})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{OAuthBaseURL: server.URL, ClientID: "client"}).RefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got.CharacterID != 987 || got.OwnerHash != "owner-hash" || got.ExpiresIn.Seconds() != 1200 || got.RefreshToken != "new-refresh" {
		t.Fatalf("token = %+v", got)
	}
}

func testJWT(t *testing.T, claims map[string]string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
