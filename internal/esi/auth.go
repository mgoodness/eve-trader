package esi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TokenResponse is the EVE SSO token endpoint's response shape, for both
// the authorization-code exchange and the refresh grant
// (docs/research/esi-sso-cli.md §3.1, §3.2).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// accessTokenClaims is the subset of an ESI access token's JWT payload
// CharacterIDFromAccessToken and CharacterNameFromAccessToken need (spec
// §12; docs/research/esi-sso-cli.md §4.3's full example payload also
// carries scp, aud, exp, etc., all irrelevant here).
type accessTokenClaims struct {
	Sub  string `json:"sub"`
	Name string `json:"name"`
}

// decodeAccessTokenClaims decodes an ESI access token's JWT payload without
// verifying the signature -- both CharacterIDFromAccessToken and
// CharacterNameFromAccessToken are callers of this one shared decode so
// there is a single place that knows the token is a 3-segment JWT and how
// to base64url-decode its middle segment.
func decodeAccessTokenClaims(accessToken string) (accessTokenClaims, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return accessTokenClaims{}, fmt.Errorf("access token %q is not a JWT (want 3 dot-separated segments, got %d)", accessToken, len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessTokenClaims{}, fmt.Errorf("decoding access token payload: %w", err)
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return accessTokenClaims{}, fmt.Errorf("parsing access token payload: %w", err)
	}
	return claims, nil
}

// CharacterIDFromAccessToken decodes the character id from an ESI access
// token's JWT payload (spec §12: "the access token is a JWT ... carrying
// the character id in sub"; docs/research/esi-sso-cli.md §4.3). The sub
// claim's observed shape is "CHARACTER:EVE:<character-id>"; per the
// research note's recommendation, this splits on ":" and takes index 2
// rather than matching a prefix, so either claim ordering CCP has
// documented works.
func CharacterIDFromAccessToken(accessToken string) (int32, error) {
	claims, err := decodeAccessTokenClaims(accessToken)
	if err != nil {
		return 0, err
	}

	fields := strings.Split(claims.Sub, ":")
	if len(fields) != 3 {
		return 0, fmt.Errorf("sub claim %q is not CHARACTER:EVE:<character-id>-shaped", claims.Sub)
	}

	id, err := strconv.ParseInt(fields[2], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("sub claim %q does not end in a character id: %w", claims.Sub, err)
	}
	return int32(id), nil
}

// CharacterNameFromAccessToken decodes the character name from an ESI
// access token's JWT payload's name claim (docs/research/esi-sso-cli.md
// §4.3's example payload: "name": "Some Bloke"). `login` prints this
// rather than the numeric character id so its output is readable at a
// glance.
func CharacterNameFromAccessToken(accessToken string) (string, error) {
	claims, err := decodeAccessTokenClaims(accessToken)
	if err != nil {
		return "", err
	}
	if claims.Name == "" {
		return "", fmt.Errorf("access token has no name claim")
	}
	return claims.Name, nil
}

// RefreshAccessToken mints a new access token from a stored refresh token
// (spec §12; docs/research/esi-sso-cli.md §3.2): POST
// https://login.eveonline.com/v2/oauth/token, form-encoded, no client
// secret (PKCE, no Authorization header). The refresh token in the
// response may differ from the one sent — callers must persist whatever
// comes back (spec §12: "rotates — persist the returned one every time").
func (c *Client) RefreshAccessToken(ctx context.Context, clientID, refreshToken string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	return c.postTokenRequest(ctx, form)
}

// ExchangeAuthorizationCode exchanges an authorization code for an
// access/refresh token pair (spec §12; docs/research/esi-sso-cli.md §1.2,
// §1.3): POST https://login.eveonline.com/v2/oauth/token, form-encoded,
// with the PKCE code_verifier and no client secret (public PKCE client,
// no Authorization header) — reuses postTokenRequest so this and
// RefreshAccessToken cannot drift.
func (c *Client) ExchangeAuthorizationCode(ctx context.Context, clientID, redirectURI, code, codeVerifier string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"client_id":     {clientID},
	}
	return c.postTokenRequest(ctx, form)
}

// postTokenRequest POSTs form, form-encoded, to the SSO token endpoint
// (docs/research/esi-sso-cli.md §3.1, §3.2) and decodes the JSON response
// as a TokenResponse. It is shared by RefreshAccessToken's refresh grant
// and the authorization-code exchange, which differ only in the form
// values they send, so the two paths cannot drift.
func (c *Client) postTokenRequest(ctx context.Context, form url.Values) (TokenResponse, error) {
	reqURL := c.ssoBaseURL + "/v2/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TokenResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return TokenResponse{}, fmt.Errorf("POST %s: unexpected status %s: %s", reqURL, resp.Status, body)
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return TokenResponse{}, fmt.Errorf("decoding token response: %w", err)
	}
	return token, nil
}
