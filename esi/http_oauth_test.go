package esi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
)

func TestHTTPGatewayExchangeCodeSendsPKCEParameters(t *testing.T) {
	accessToken := testJWT(t, map[string]string{"sub": "CHARACTER:EVE:123"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"grant_type": "authorization_code",
			"code":       "code", "client_id": "client", "redirect_uri": "http://localhost/callback", "code_verifier": "verifier",
		}
		for key, value := range want {
			if got := r.Form.Get(key); got != value {
				t.Errorf("form[%q] = %q, want %q", key, got, value)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": accessToken, "expires_in": 60})
	}))
	defer server.Close()

	got, err := (&esi.HTTPGateway{
		OAuthBaseURL: server.URL,
		ClientID:     "client", CallbackURL: "http://localhost/callback",
	}).ExchangeCode(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if got.CharacterID != 123 || got.AccessToken != accessToken {
		t.Fatalf("token = %+v", got)
	}
}
