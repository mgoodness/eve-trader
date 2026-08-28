// Package auth implements EVE SSO login: the authorize redirect, the
// authorization-code exchange, and refresh-token persistence/rotation.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

const authorizeURL = "https://login.eveonline.com/v2/oauth/authorize"

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
