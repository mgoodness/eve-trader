// Package auth implements EVE SSO login: the authorize redirect, the
// authorization-code exchange, and refresh-token persistence/rotation.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

const authorizeURL = "https://login.eveonline.com/v2/oauth/authorize"

// tokenRefreshMargin is how far ahead of an access token's actual expiry
// NewTokenSource proactively refreshes it, so a request in flight doesn't
// race the token's expiry.
const tokenRefreshMargin = 1 * time.Minute

// Scopes are the ESI scopes eve-trader requests at login (see the parent
// spec, issue #13: markets/character-orders, skills, and standings, the
// latter two needed for fee computation per ADR-0001).
var Scopes = []string{
	"esi-markets.read_character_orders.v1",
	"esi-skills.read_skills.v1",
	"esi-characters.read_standings.v1",
}

// LoginURL builds the EVE SSO authorize redirect URL for the given
// application and CSRF-protection state value.
func LoginURL(clientID, callbackURL, state string) string {
	v := url.Values{
		"response_type": {"code"},
		"client_id":     {clientID},
		"redirect_uri":  {callbackURL},
		"scope":         {strings.Join(Scopes, " ")},
		"state":         {state},
	}
	return authorizeURL + "?" + v.Encode()
}

// RandomState returns a CSRF-protection state value for the login
// redirect.
func RandomState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Exchange completes an OAuth authorization-code exchange and persists the
// resulting token.
func Exchange(ctx context.Context, client esi.Client, store *storage.Store, code string) (esi.Token, error) {
	token, err := client.ExchangeCode(ctx, code)
	if err != nil {
		return esi.Token{}, fmt.Errorf("exchange code: %w", err)
	}
	if err := persist(ctx, store, token); err != nil {
		return esi.Token{}, err
	}
	return token, nil
}

// Refresh redeems the stored refresh token for a new access token and
// persists whatever comes back. ESI may return a different refresh token
// than the one submitted on any call, so the newest value always replaces
// what's stored (see ADR-0003).
func Refresh(ctx context.Context, client esi.Client, store *storage.Store) (esi.Token, error) {
	current, err := store.LoadToken(ctx)
	if err != nil {
		return esi.Token{}, fmt.Errorf("load stored token: %w", err)
	}

	token, err := client.RefreshAccessToken(ctx, current.RefreshToken)
	if err != nil {
		return esi.Token{}, fmt.Errorf("refresh access token: %w", err)
	}
	if err := persist(ctx, store, token); err != nil {
		return esi.Token{}, err
	}
	return token, nil
}

// NewTokenSource returns an esi.TokenSource backed by the stored token,
// transparently refreshing (and persisting the rotated result, per
// ADR-0003) whenever the stored access token is at or near expiry.
func NewTokenSource(client esi.Client, store *storage.Store) esi.TokenSource {
	return func(ctx context.Context) (string, error) {
		token, err := store.LoadToken(ctx)
		if err != nil {
			return "", fmt.Errorf("load token: %w", err)
		}

		if time.Now().Add(tokenRefreshMargin).Before(token.ExpiresAt) {
			return token.AccessToken, nil
		}

		refreshed, err := Refresh(ctx, client, store)
		if err != nil {
			return "", fmt.Errorf("refresh token: %w", err)
		}
		return refreshed.AccessToken, nil
	}
}

// CharacterIDFromAccessToken extracts the character ID from an EVE SSO
// access token's `sub` claim (format "CHARACTER:EVE:<id>"). The token was
// obtained directly from login.eveonline.com over TLS during our own
// exchange/refresh call, so it's trusted by construction -- this reads the
// claim without independently verifying the JWT's signature.
func CharacterIDFromAccessToken(accessToken string) (int32, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("access token is not a JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, fmt.Errorf("decode access token payload: %w", err)
	}

	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, fmt.Errorf("decode access token claims: %w", err)
	}

	const prefix = "CHARACTER:EVE:"
	if !strings.HasPrefix(claims.Sub, prefix) {
		return 0, fmt.Errorf("unexpected sub claim format: %q", claims.Sub)
	}

	id, err := strconv.ParseInt(strings.TrimPrefix(claims.Sub, prefix), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse character id from sub claim: %w", err)
	}
	return int32(id), nil
}

// CurrentCharacterID returns the character ID of whichever character is
// currently authenticated, derived from the stored access token.
func CurrentCharacterID(ctx context.Context, store *storage.Store) (int32, error) {
	token, err := store.LoadToken(ctx)
	if err != nil {
		return 0, fmt.Errorf("load token: %w", err)
	}
	return CharacterIDFromAccessToken(token.AccessToken)
}

func persist(ctx context.Context, store *storage.Store, token esi.Token) error {
	if err := store.SaveToken(ctx, storage.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Add(token.ExpiresIn),
	}); err != nil {
		return fmt.Errorf("persist token: %w", err)
	}
	return nil
}
