package esi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
)

// Scopes is the exact, fixed scope set eve-trader requests (spec §12):
// just enough to read skills and standings for live fee/order-limit
// derivation.
const Scopes = "esi-skills.read_skills.v1 esi-characters.read_standings.v1"

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
// Scopes, S256 PKCE, and the caller-supplied state and redirect URI.
func AuthorizeURL(ssoBaseURL, clientID, redirectURI, state, codeChallenge string) string {
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
