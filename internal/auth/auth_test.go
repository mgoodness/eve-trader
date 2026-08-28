package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// fakeAccessToken builds a JWT-shaped string with the given `sub` claim in
// its (unsigned, for test purposes) payload segment -- enough to exercise
// CharacterIDFromAccessToken without a real EVE SSO token.
func fakeAccessToken(t *testing.T, sub string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"sub": sub})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return "header." + encoded + ".signature"
}

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

func TestCharacterIDFromAccessToken(t *testing.T) {
	token := fakeAccessToken(t, "CHARACTER:EVE:95465499")

	id, err := CharacterIDFromAccessToken(token)
	if err != nil {
		t.Fatalf("CharacterIDFromAccessToken: %v", err)
	}
	if id != 95465499 {
		t.Errorf("id = %d, want 95465499", id)
	}
}

func TestCharacterIDFromAccessTokenMalformed(t *testing.T) {
	for name, token := range map[string]string{
		"not a JWT":         "not-a-jwt",
		"bad sub format":    fakeAccessToken(t, "not-the-right-shape"),
		"non-numeric id":    fakeAccessToken(t, "CHARACTER:EVE:abc"),
		"unencoded payload": "header.not-base64!!!.signature",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CharacterIDFromAccessToken(token); err == nil {
				t.Fatalf("CharacterIDFromAccessToken(%q): want error, got nil", token)
			}
		})
	}
}

func TestTokenSourceReturnsStoredTokenWhenNotExpiringSoon(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.SaveToken(ctx, storage.Token{
		AccessToken:  "still-good",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	fake := esi.NewFakeClient()
	fake.TokenResp = esi.Token{AccessToken: "should-not-be-used", RefreshToken: "should-not-be-used"}

	ts := NewTokenSource(fake, store)
	got, err := ts(ctx)
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if got != "still-good" {
		t.Errorf("TokenSource() = %q, want still-good (no refresh needed)", got)
	}
}

func TestTokenSourceRefreshesWhenExpiringSoon(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.SaveToken(ctx, storage.Token{
		AccessToken:  "stale",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	fake := esi.NewFakeClient()
	fake.TokenResp = esi.Token{
		AccessToken:  "fresh",
		RefreshToken: "new-refresh",
		ExpiresIn:    20 * time.Minute,
	}

	ts := NewTokenSource(fake, store)
	got, err := ts(ctx)
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if got != "fresh" {
		t.Errorf("TokenSource() = %q, want fresh", got)
	}

	persisted, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if persisted.RefreshToken != "new-refresh" {
		t.Errorf("persisted RefreshToken = %q, want new-refresh", persisted.RefreshToken)
	}
}

func TestCurrentCharacterID(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.SaveToken(ctx, storage.Token{
		AccessToken:  fakeAccessToken(t, "CHARACTER:EVE:95465499"),
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(20 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	id, err := CurrentCharacterID(ctx, store)
	if err != nil {
		t.Fatalf("CurrentCharacterID: %v", err)
	}
	if id != 95465499 {
		t.Errorf("id = %d, want 95465499", id)
	}
}

func TestCurrentCharacterIDWithNoStoredTokenFails(t *testing.T) {
	store := openStore(t)

	if _, err := CurrentCharacterID(context.Background(), store); err == nil {
		t.Fatal("CurrentCharacterID: want error when no token has been saved yet, got nil")
	}
}

func TestTokenSourceWithNoStoredTokenFails(t *testing.T) {
	store := openStore(t)
	fake := esi.NewFakeClient()

	ts := NewTokenSource(fake, store)
	if _, err := ts(context.Background()); err == nil {
		t.Fatal("TokenSource: want error when no token has been saved yet, got nil")
	}
}
