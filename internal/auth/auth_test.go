package auth

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

func openStore(t *testing.T) *storage.Store {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return store
}

func TestLoginURL(t *testing.T) {
	raw := LoginURL("my-client-id", "https://eve-trader.opsgoodness.net/auth/callback", "state-xyz")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://login.eveonline.com/v2/oauth/authorize" {
		t.Errorf("base URL = %q, want the EVE SSO authorize endpoint", u.Scheme+"://"+u.Host+u.Path)
	}

	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("client_id") != "my-client-id" {
		t.Errorf("client_id = %q, want my-client-id", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://eve-trader.opsgoodness.net/auth/callback" {
		t.Errorf("redirect_uri = %q, want the callback URL", q.Get("redirect_uri"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("state = %q, want state-xyz", q.Get("state"))
	}

	gotScopes := strings.Fields(q.Get("scope"))
	wantScopes := []string{
		"esi-markets.read_character_orders.v1",
		"esi-skills.read_skills.v1",
		"esi-characters.read_standings.v1",
	}
	if len(gotScopes) != len(wantScopes) {
		t.Fatalf("scopes = %v, want %v", gotScopes, wantScopes)
	}
	for _, want := range wantScopes {
		found := false
		for _, got := range gotScopes {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("scopes %v missing %q", gotScopes, want)
		}
	}
}

func TestRandomStateIsUnpredictableAndNonEmpty(t *testing.T) {
	a, err := RandomState()
	if err != nil {
		t.Fatalf("RandomState: %v", err)
	}
	b, err := RandomState()
	if err != nil {
		t.Fatalf("RandomState: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("RandomState returned an empty value")
	}
	if a == b {
		t.Fatal("RandomState returned the same value twice")
	}
}

func TestExchangePersistsToken(t *testing.T) {
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.TokenResp = esi.Token{
		AccessToken:  "first-access",
		RefreshToken: "first-refresh",
		ExpiresIn:    20 * time.Minute,
	}

	if _, err := Exchange(context.Background(), fake, store, "auth-code"); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	got, err := store.LoadToken(context.Background())
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.RefreshToken != "first-refresh" {
		t.Errorf("RefreshToken = %q, want first-refresh", got.RefreshToken)
	}
}

// TestRefreshPersistsRotatedToken is the AC-mandated test: given a fake
// ESIClient that returns a *different* refresh token on refresh, the
// newest one -- not the one that was passed in -- is what ends up
// persisted (see ADR-0003: ESI rotates the refresh token on every use).
func TestRefreshPersistsRotatedToken(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.SaveToken(ctx, storage.Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
	}); err != nil {
		t.Fatalf("seed SaveToken: %v", err)
	}

	fake := esi.NewFakeClient()
	fake.TokenResp = esi.Token{
		AccessToken:  "rotated-access",
		RefreshToken: "rotated-refresh",
		ExpiresIn:    20 * time.Minute,
	}

	got, err := Refresh(ctx, fake, store)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got.RefreshToken != "rotated-refresh" {
		t.Errorf("Refresh() RefreshToken = %q, want rotated-refresh", got.RefreshToken)
	}

	persisted, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if persisted.RefreshToken != "rotated-refresh" {
		t.Errorf("persisted RefreshToken = %q, want rotated-refresh (the new one, not old-refresh)", persisted.RefreshToken)
	}
	if persisted.AccessToken != "rotated-access" {
		t.Errorf("persisted AccessToken = %q, want rotated-access", persisted.AccessToken)
	}
}

func TestRefreshWithNoStoredTokenFails(t *testing.T) {
	store := openStore(t)
	fake := esi.NewFakeClient()

	if _, err := Refresh(context.Background(), fake, store); err == nil {
		t.Fatal("Refresh: want error when no token has been saved yet, got nil")
	}
}
