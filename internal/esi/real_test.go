package esi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func newTestRealClient(t *testing.T, handler http.HandlerFunc) *RealClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewRealClient("test-client-id", "test-client-secret")
	c.TokenURL = srv.URL
	return c
}

func TestExchangeCode(t *testing.T) {
	var gotForm url.Values
	var gotUser, gotPass string

	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		gotUser, gotPass, _ = r.BasicAuth()

		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    1199,
		})
	})

	token, err := client.ExchangeCode(context.Background(), "auth-code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	if gotUser != "test-client-id" || gotPass != "test-client-secret" {
		t.Errorf("basic auth = %q:%q, want test-client-id:test-client-secret", gotUser, gotPass)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "auth-code-123" {
		t.Errorf("code = %q, want auth-code-123", gotForm.Get("code"))
	}

	if token.AccessToken != "new-access-token" {
		t.Errorf("AccessToken = %q, want new-access-token", token.AccessToken)
	}
	if token.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken = %q, want new-refresh-token", token.RefreshToken)
	}
	if token.ExpiresIn != 1199*time.Second {
		t.Errorf("ExpiresIn = %v, want %v", token.ExpiresIn, 1199*time.Second)
	}
}

func TestRefreshAccessToken(t *testing.T) {
	var gotForm url.Values

	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "rotated-access-token",
			"refresh_token": "rotated-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    1199,
		})
	})

	token, err := client.RefreshAccessToken(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}

	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
	}
	if gotForm.Get("refresh_token") != "old-refresh-token" {
		t.Errorf("refresh_token = %q, want old-refresh-token", gotForm.Get("refresh_token"))
	}
	if token.RefreshToken != "rotated-refresh-token" {
		t.Errorf("RefreshToken = %q, want rotated-refresh-token", token.RefreshToken)
	}
}

func TestTokenRequestErrorStatus(t *testing.T) {
	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	if _, err := client.ExchangeCode(context.Background(), "bad-code"); err == nil {
		t.Fatal("ExchangeCode: want error on non-200 response, got nil")
	}
}
