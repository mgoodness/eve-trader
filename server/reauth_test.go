package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
	"github.com/mgoodness/eve-trader/server"
)

type countingGateway struct {
	calls atomic.Int32
	*esi.Fake
}

func (g *countingGateway) RefreshToken(ctx context.Context, refreshToken string) (esi.Token, error) {
	g.calls.Add(1)
	return esi.Token{}, errors.New("refresh token revoked")
}

func TestIndexShowsReauthenticationOnFirstBootWithoutToken(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := httptest.NewServer(server.New(gateway, sqlDB, testAuthConfig()))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "Re-authenticate with EVE") {
		t.Fatalf("GET / body missing re-authentication banner on first boot with no stored token:\n%s", body)
	}
	if strings.Contains(body, "id=\"rows\"") {
		t.Errorf("GET / rendered opportunity table with no stored token")
	}

	// No token means nothing to refresh, and the latched state must not
	// trigger repeated refresh attempts across requests.
	_, _ = getBody(t, srv.URL+"/")
	if got := gateway.calls.Load(); got != 0 {
		t.Errorf("RefreshToken calls = %d, want 0 with no stored token", got)
	}
}

func TestIndexShowsReauthenticationAfterRefreshFailure(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	ciphertext, err := tokencrypt.Encrypt(testAuthConfig().TokenKey, "revoked-refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at) VALUES (?, ?, ?, ?)`, 1, "owner", ciphertext, "now"); err != nil {
		t.Fatal(err)
	}

	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := httptest.NewServer(server.New(gateway, sqlDB, testAuthConfig()))
	defer srv.Close()

	status, body := getBody(t, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "Re-authenticate with EVE") {
		t.Fatalf("GET / body missing re-authentication banner:\n%s", body)
	}
	if strings.Contains(body, "id=\"rows\"") {
		t.Errorf("GET / rendered opportunity table while re-authentication is required")
	}

	_, _ = getBody(t, srv.URL+"/")
	if got := gateway.calls.Load(); got != 1 {
		t.Errorf("RefreshToken calls = %d, want exactly one after failure", got)
	}
}
