package esi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
)

// Scopes is the exact, fixed scope set eve-trader requests. The first two
// (spec §12) read skills and standings for live fee/order-limit derivation;
// the latter three (wayfinder map "eve-trader inventory-aware
// recommendations", ticket "Provision expanded ESI scopes and
// re-authorize the CLI token") read assets, character orders, and wallet
// transactions for the inventory ledger's reconciliation and cost-basis
// work.
const Scopes = "esi-skills.read_skills.v1 esi-characters.read_standings.v1 esi-assets.read_assets.v1 esi-markets.read_character_orders.v1 esi-wallet.read_character_wallet.v1"

// GeneratePKCE returns a fresh PKCE verifier/challenge pair, generated per
// EVE SSO's non-RFC-7636 behaviour (docs/research/esi-sso-cli.md §1.3):
// the verifier is the base64url *string* of 32 random bytes — padded, per
// CCP's own snippet, not the raw bytes RFC 7636 expects — and the
// challenge is derived from it by PKCEChallenge.
func GeneratePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generating PKCE verifier: %w", err)
	}
	verifier = base64.URLEncoding.EncodeToString(raw)
	return verifier, PKCEChallenge(verifier), nil
}

// PKCEChallenge derives the S256 code challenge for verifier, per EVE
// SSO's non-RFC-7636 behaviour (docs/research/esi-sso-cli.md §1.3):
// base64url(SHA-256(ASCII(verifier string))), unpadded. Follow CCP's
// snippet exactly — hashing the decoded verifier bytes instead (the RFC
// 7636-conformant behaviour) produces a challenge the server rejects.
// Exposed separately from GeneratePKCE so tests can assert it against a
// fixed vector without depending on randomness.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizeURL builds the EVE SSO authorization request URL (spec §12;
// docs/research/esi-sso-cli.md §1.2): response_type=code, the fixed
// Scopes, S256 PKCE, and the caller-supplied state and redirect URI. An
// empty ssoBaseURL defaults to the production SSO host, exactly like
// NewClient -- cli.DefaultConfig leaves SSOBaseURL at its zero value and
// relies on that default, and this is the one caller (internal/cli/login.go)
// that builds a URL directly rather than going through a Client, so it must
// honor the same empty-means-default contract or production gets a
// relative, unopenable URL (diagnosed: a bare "" reached here and produced
// "/v2/oauth/authorize?..." with no scheme or host).
func AuthorizeURL(ssoBaseURL, clientID, redirectURI, state, codeChallenge string) string {
	if ssoBaseURL == "" {
		ssoBaseURL = defaultSSOBaseURL
	}

	v := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {Scopes},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return ssoBaseURL + "/v2/oauth/authorize?" + v.Encode()
}
