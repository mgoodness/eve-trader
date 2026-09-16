// auth.go implements the one-time EVE SSO login flow (docs/spec/v1.md
// §6): /auth/login redirects into EVE's PKCE OAuth flow, /auth/callback
// exchanges the code for a token via ESIGateway.ExchangeCode, and the
// resulting refresh token plus the character's fetched skills are
// persisted.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
)

// AuthConfig holds the settings the EVE SSO login flow needs: the
// developer app's client ID and registered callback URL, and the two
// secrets that protect the flow (§6). CookieSecret HMAC-signs the
// short-lived PKCE cookie; TokenKey AES-GCM-encrypts the persisted
// refresh token. Both are arbitrary-length strings -- each is hashed to a
// fixed-size key internally, so operator setup (a single environment
// variable per secret) stays simple.
type AuthConfig struct {
	ClientID     string
	CallbackURL  string
	CookieSecret string
	TokenKey     string
}

const (
	ssoAuthorizeEndpoint = "https://login.eveonline.com/v2/oauth/authorize"
	ssoScope             = "esi-skills.read_skills.v1"

	pkceCookieName = "eve_trader_pkce"
	pkceCookiePath = "/auth"
	pkceCookieTTL  = 5 * time.Minute
)

// pkceState is the payload held in the short-lived, HMAC-signed PKCE
// cookie across the /auth/login -> /auth/callback redirect round-trip.
// Held in a cookie rather than server-side session state so the flow
// survives an app restart mid-login (§6), provided AuthConfig.CookieSecret
// itself is a persistent value (see main.go's ephemeral-secret fallback,
// which deliberately doesn't have this property).
type pkceState struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"expires"` // unix seconds
}

// handleAuthLogin redirects to EVE SSO's authorization endpoint,
// requesting only the esi-skills.read_skills.v1 scope, with a PKCE (S256)
// challenge. The matching state/code_verifier are held in a short-lived,
// HMAC-signed cookie -- there is no server-side session store.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth login: generating PKCE challenge", "starting login", err)
		return
	}

	state, err := randomToken(16)
	if err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth login: generating state", "starting login", err)
		return
	}

	if err := s.setPKCECookie(w, pkceState{State: state, Verifier: verifier, Expires: time.Now().Add(pkceCookieTTL).Unix()}); err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth login: setting PKCE cookie", "starting login", err)
		return
	}

	http.Redirect(w, r, s.ssoAuthorizeURL(state, challenge), http.StatusFound)
}

// handleAuthCallback validates the PKCE cookie and state, exchanges the
// authorization code for a token via ESIGateway.ExchangeCode, and on
// success persists the encrypted refresh token and the character's
// fetched skills.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	// The PKCE cookie is single-use: clear it regardless of outcome.
	defer s.clearPKCECookie(w)

	pkce, err := s.verifyPKCECookie(r)
	if err != nil {
		s.authFail(w, http.StatusBadRequest, "auth callback: invalid PKCE cookie", "login session expired or invalid, please try again", err)
		return
	}

	q := r.URL.Query()
	if state := q.Get("state"); state == "" || state != pkce.State {
		s.authFail(w, http.StatusBadRequest, "auth callback: state mismatch", "invalid login state", nil)
		return
	}

	code := q.Get("code")
	if code == "" {
		s.authFail(w, http.StatusBadRequest, "auth callback: missing authorization code", "missing authorization code", nil)
		return
	}

	ctx := r.Context()

	token, err := s.gateway.ExchangeCode(ctx, code, pkce.Verifier)
	if err != nil {
		s.authFail(w, http.StatusBadGateway, "auth callback: exchanging code", "exchanging authorization code", err)
		return
	}

	encrypted, err := tokencrypt.Encrypt(s.auth.TokenKey, token.RefreshToken)
	if err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth callback: encrypting refresh token", "storing token", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.upsertESIToken(ctx, token, encrypted, now); err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth callback: storing esi_token", "storing token", err)
		return
	}

	// Access tokens are never written to disk -- only used in-memory,
	// here, to fetch the character's skills.
	skills, err := s.gateway.FetchCharacterSkills(ctx, token.CharacterID, token.AccessToken)
	if err != nil {
		s.authFail(w, http.StatusBadGateway, "auth callback: fetching character skills", "fetching character skills", err)
		return
	}

	if err := s.upsertCharacterSkill(ctx, token.CharacterID, skills, now); err != nil {
		s.authFail(w, http.StatusInternalServerError, "auth callback: storing character_skill", "storing character skills", err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

// authFail logs msg (with err, if non-nil) and writes userMsg to w as an
// HTTP error with status. It centralizes the log-then-respond shape every
// failure branch in this file needs.
func (s *Server) authFail(w http.ResponseWriter, status int, msg, userMsg string, err error) {
	if err != nil {
		slog.Error(msg, "err", err)
	} else {
		slog.Error(msg)
	}
	http.Error(w, userMsg, status)
}

// upsertESIToken stores (or replaces) the single esi_token row for
// token.CharacterID.
func (s *Server) upsertESIToken(ctx context.Context, token esi.Token, encryptedRefreshToken []byte, updatedAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (character_id) DO UPDATE SET
		   owner_hash = excluded.owner_hash,
		   encrypted_refresh_token = excluded.encrypted_refresh_token,
		   updated_at = excluded.updated_at`,
		token.CharacterID, token.OwnerHash, encryptedRefreshToken, updatedAt,
	)
	return err
}

// upsertCharacterSkill stores (or replaces) the single character_skill
// row for characterID.
func (s *Server) upsertCharacterSkill(ctx context.Context, characterID int, skills esi.Skills, updatedAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO character_skill (character_id, broker_relations_level, accounting_level, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (character_id) DO UPDATE SET
		   broker_relations_level = excluded.broker_relations_level,
		   accounting_level = excluded.accounting_level,
		   updated_at = excluded.updated_at`,
		characterID, skills.BrokerRelationsLevel, skills.AccountingLevel, updatedAt,
	)
	return err
}

// ssoAuthorizeURL builds the EVE SSO v2 authorization-endpoint URL for
// state/challenge, requesting only the esi-skills.read_skills.v1 scope
// with a PKCE S256 challenge.
func (s *Server) ssoAuthorizeURL(state, challenge string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", s.auth.ClientID)
	v.Set("redirect_uri", s.auth.CallbackURL)
	v.Set("scope", ssoScope)
	v.Set("state", state)
	v.Set("code_challenge", challenge)
	v.Set("code_challenge_method", "S256")
	return ssoAuthorizeEndpoint + "?" + v.Encode()
}

// generatePKCE returns a fresh PKCE code_verifier and its S256
// code_challenge (RFC 7636).
func generatePKCE() (verifier, challenge string, err error) {
	verifier, err = randomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generating code_verifier: %w", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// randomToken returns n cryptographically random bytes, base64url-encoded
// (no padding).
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setPKCECookie sets the short-lived, HMAC-signed PKCE cookie carrying
// pkce across the /auth/login -> /auth/callback redirect.
//
// The cookie is deliberately not marked Secure: production terminates
// TLS at a reverse proxy (Caddy, docs/spec/v1.md §8) in front of this
// plain-HTTP backend, so the backend has no reliable signal (r.TLS is
// always nil) that the browser's connection to the proxy was HTTPS
// without trusting a forwarded-proto header -- not worth the added
// trust surface to protect a cookie that only carries an ephemeral PKCE
// state/verifier pair, not the refresh token itself. HttpOnly, a strict
// SameSite policy, and the short TTL are judged sufficient for this
// single-user tool's threat model.
func (s *Server) setPKCECookie(w http.ResponseWriter, pkce pkceState) error {
	payload, err := json.Marshal(pkce)
	if err != nil {
		return fmt.Errorf("marshaling PKCE cookie: %w", err)
	}

	sig := s.signPKCEPayload(payload)
	value := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, &http.Cookie{
		Name:     pkceCookieName,
		Value:    value,
		Path:     pkceCookiePath,
		MaxAge:   int(pkceCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// clearPKCECookie deletes the PKCE cookie; it is single-use.
func (s *Server) clearPKCECookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pkceCookieName,
		Value:    "",
		Path:     pkceCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// verifyPKCECookie reads, verifies the HMAC signature of, and checks the
// expiry of the PKCE cookie on r.
func (s *Server) verifyPKCECookie(r *http.Request) (pkceState, error) {
	c, err := r.Cookie(pkceCookieName)
	if err != nil {
		return pkceState{}, fmt.Errorf("reading PKCE cookie: %w", err)
	}

	payloadPart, sigPart, ok := strings.Cut(c.Value, ".")
	if !ok {
		return pkceState{}, errors.New("malformed PKCE cookie")
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return pkceState{}, fmt.Errorf("decoding PKCE cookie payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return pkceState{}, fmt.Errorf("decoding PKCE cookie signature: %w", err)
	}

	if !hmac.Equal(sig, s.signPKCEPayload(payload)) {
		return pkceState{}, errors.New("invalid PKCE cookie signature")
	}

	var pkce pkceState
	if err := json.Unmarshal(payload, &pkce); err != nil {
		return pkceState{}, fmt.Errorf("decoding PKCE cookie: %w", err)
	}

	if time.Now().Unix() > pkce.Expires {
		return pkceState{}, errors.New("expired PKCE cookie")
	}

	return pkce, nil
}

// signPKCEPayload HMAC-SHA256-signs payload with a key derived from
// AuthConfig.CookieSecret.
func (s *Server) signPKCEPayload(payload []byte) []byte {
	key := sha256.Sum256([]byte(s.auth.CookieSecret))
	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	return mac.Sum(nil)
}
