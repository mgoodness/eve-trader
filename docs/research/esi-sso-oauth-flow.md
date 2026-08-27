# Research: EVE SSO / ESI OAuth flow, scopes, and token refresh

Resolves [issue #8](https://github.com/mgoodness/eve-trader/issues/8), part of the wayfinder map ([issue #1](https://github.com/mgoodness/eve-trader/issues/1)).

Sources are primary: the current developer docs at
[developers.eveonline.com/docs](https://developers.eveonline.com/docs/), the legacy-but-still-live
`docs.esi.evetech.net` reference pages (marked deprecated in favor of the site above, but still
served and cited here where they contain detail not yet migrated), and the live ESI OpenAPI spec
served at `https://esi.evetech.net/meta/openapi.json`, queried directly.

## 1. Registering a developer application

Register at [developers.eveonline.com/applications](https://developers.eveonline.com/applications)
(requires logging in with an EVE Online account first). Creating a new application asks for:

- **Name** and **Description** — free text, informational only.
- **Connection Type** — choose **"Authentication & API Access"** for ESI access (this is what
  exposes the scope picker). ("Authentication Only" exists for apps that just need login/identity
  and no ESI scopes.)
- **Callback URL** — a single redirect URL for this application (see §4 below for constraints).
- **Permissions / scopes** — once "Authentication & API Access" is selected, an "Available Scopes
  List" is presented; click a scope to move it into the "Requested Scopes List." Request only the
  scopes actually needed.

On creation, "View Application" shows the **Client ID** (public, safe to embed/share) and **Secret
Key** (must be kept private — used for confidential/web-app-style token exchange).

Important operational note: **changing an application's requested scopes invalidates existing
refresh tokens** for that app — users must redo the SSO flow.

Sources: [Single Sign-On (SSO)](https://developers.eveonline.com/docs/services/sso/),
[creating_sso_application (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/creating_sso_application.html)

## 2. OAuth2 flow — exact endpoints and parameters

EVE SSO is an OAuth 2.0 **authorization code flow** (PKCE-capable for native apps; confidential
client-secret flow for server-side/web apps). Endpoint URLs can also be discovered dynamically
(recommended, and cacheable) from the metadata document at
`https://login.eveonline.com/.well-known/oauth-authorization-server`.

**Authorization endpoint:**
```
https://login.eveonline.com/v2/oauth/authorize
```
Query parameters:
- `response_type=code`
- `redirect_uri` — must exactly match a callback URL registered on the application
- `client_id`
- `scope` — space-delimited list of requested ESI scopes, URL-encoded (may be omitted/empty for a
  scopeless login — see §3)
- `state` — random string for CSRF protection

**Token endpoint (authorization code exchange):**
```
POST https://login.eveonline.com/v2/oauth/token
Content-Type: application/x-www-form-urlencoded
Authorization: Basic base64(client_id:secret_key)   # web/confidential apps

grant_type=authorization_code&code=<authorization code>
```
Native (public) apps that can't hold a secret omit the `Authorization` header and instead include
`client_id` in the body (and typically use PKCE).

Response:
```json
{
  "access_token": "<JWT>",
  "expires_in": 1199,
  "token_type": "Bearer",
  "refresh_token": "<opaque string>"
}
```

The token endpoint **requires** `Content-Type: application/x-www-form-urlencoded` per RFC 6749
§4.1.3 — JSON bodies and query-string parameters are explicitly rejected. The authorization code
itself is single-use and short-lived (5 minutes).

Sources: [Single Sign-On (SSO)](https://developers.eveonline.com/docs/services/sso/),
[web_based_sso_flow (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/web_based_sso_flow.html),
[SSO Endpoint Deprecations blog post](https://developers.eveonline.com/blog/sso-endpoint-deprecations-2)

## 3. Scopes

Confirmed directly against the live ESI OpenAPI spec (`GET https://esi.evetech.net/meta/openapi.json`,
`components.securitySchemes.OAuth2`, and the `security` block on each operation):

- `GET /characters/{character_id}/orders` (list a character's **open** market orders) requires
  scope: **`esi-markets.read_character_orders.v1`**
- `GET /characters/{character_id}/orders/history` (historical/closed orders) requires the **same**
  scope: `esi-markets.read_character_orders.v1`

These are the exact strings as they appear in `securitySchemes.OAuth2.flows.authorizationCode.scopes`
in the live spec — verified by fetching it directly rather than trusting a cached/secondary listing.

**Identity/login requires no ESI scope.** The `scope` parameter on the authorize URL is optional;
an empty/omitted scope still completes the OAuth flow and returns a valid access token whose JWT
carries the character's identity (`sub` claim, format `CHARACTER:EVE:<character_id>`, plus a `name`
claim). CCP's own best-practice guidance is to do a **"scopeless login"** first and only step the
user up to a scoped login (via an interstitial explaining why) when a feature that needs ESI scopes
is actually used — this also means a first, no-scope login does not by itself produce a usable
refresh token for scoped calls.

Public/unauthenticated confirmation: `GET /characters/{character_id}` (the public character info
endpoint, distinct from the authenticated orders endpoints above) has `security: null` in the
OpenAPI spec — no token or scope needed at all, consistent with basic character data not requiring
ESI scopes.

Sources: live query of `https://esi.evetech.net/meta/openapi.json` (see below),
[best_practices (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/best_practices.html),
[validating_eve_jwt (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/validating_eve_jwt.html)

```
$ curl -s https://esi.evetech.net/meta/openapi.json | jq '.paths."/characters/{character_id}/orders".get.security'
[{"OAuth2": ["esi-markets.read_character_orders.v1"]}]
```

## 4. Callback URL constraints

- The redirect URL sent in the authorize request **must exactly match** one of the callback URLs
  registered on the application ("Any other URL will be rejected by the SSO service").
- Each application has (in practice, as configured via the applications UI) a single registered
  callback URL, editable after creation.
- **Localhost is explicitly supported for development**: `https://localhost/callback/` (or similar)
  is called out as acceptable while developing, with an explicit warning to **never use localhost
  for a released/production application**.
- Note the scheme in the docs' own example is `https://localhost/...`, not `http://` — treat HTTPS
  as the expected scheme even for the localhost dev case; production callback URLs must be HTTPS.

Sources: [Single Sign-On (SSO)](https://developers.eveonline.com/docs/services/sso/),
[creating_sso_application (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/creating_sso_application.html)

## 5. Token lifetimes and refresh behavior

- **Access token lifetime:** ~20 minutes. `expires_in` observed as `1199`–`1200` seconds in example
  responses across the docs.
- **Authorization code lifetime:** 5 minutes, single use.
- **Refresh token lifetime:** no fixed expiry — described as usable **"indefinitely"** as long as
  the user hasn't revoked the application's access via the EVE support site, and the app's scopes
  haven't changed (which invalidates it, per §1).
- **Refresh token rotation:** the docs explicitly warn that **the refresh token returned from a
  refresh call may differ from the one submitted** — "developers should assume that the refresh
  token MIGHT change when it is refreshed and update it when needed." Treat every refresh response
  as a potential rotation and always persist the newest `refresh_token` value. (Rotation is called
  out as mandatory/expected for native apps in particular; confidential/web apps may also see it.)

**Refreshing an access token:**
```
POST https://login.eveonline.com/v2/oauth/token
Content-Type: application/x-www-form-urlencoded
Authorization: Basic base64(client_id:secret_key)   # web/confidential apps

grant_type=refresh_token&refresh_token=<refresh_token>[&scope=<optional subset>]
```
Native apps omit the `Authorization` header and instead send `client_id` in the body. Response
shape is identical to the initial token response (new `access_token`, `expires_in`, possibly-new
`refresh_token`).

**Revoking a refresh token** (e.g. if compromised):
```
POST https://login.eveonline.com/v2/oauth/revoke
Content-Type: application/x-www-form-urlencoded
Authorization: Basic base64(client_id:secret_key)

token_type_hint=refresh_token&token=<refresh_token>
```
The endpoint returns `200 OK` whether or not the token was actually valid (so the response can't be
used to probe token validity). After revocation, the token can no longer be used to obtain new
access tokens. Users can also revoke an app's access themselves via CCP's support/account site,
independent of any API call.

Sources: [refreshing_access_tokens (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/refreshing_access_tokens.html),
[revoking_refresh_tokens (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/revoking_refresh_tokens.html),
[SSO Endpoint Deprecations blog post](https://developers.eveonline.com/blog/sso-endpoint-deprecations-2)

## 6. JWT validation

Access tokens are JWTs (RS256, ES256 support flagged as forthcoming). To validate locally without
calling SSO on every request:

- Fetch signing keys from the JWKS URL: `https://login.eveonline.com/oauth/jwks` (or discover it,
  along with the issuer and other endpoints, from
  `https://login.eveonline.com/.well-known/oauth-authorization-server`, which is safe/recommended
  to cache).
- Validate `iss` against `login.eveonline.com` or `https://login.eveonline.com` (both forms are
  valid — the docs call out that apps must accept either).
- Useful claims: `sub` (`CHARACTER:EVE:<character_id>`), `name` (character name), `owner` (account
  owner hash), `scp` (array of granted scopes), `azp` (client ID the token was issued to), `exp`/`iat`.

Source: [validating_eve_jwt (docs.esi.evetech.net)](https://docs.esi.evetech.net/docs/sso/validating_eve_jwt.html)

## 7. Rate limits / restrictions on SSO endpoints specifically

The docs don't publish a specific numeric rate limit for `/v2/oauth/authorize` or `/v2/oauth/token`
(unlike the general ESI API, which documents an explicit **error-limit window** via
`X-Esi-Error-Limit-Remain` / `X-Esi-Error-Limit-Reset` response headers and a separate sliding
"bucket" rate limit — those are ESI-data-API concerns, not SSO/token-endpoint concerns, and are out
of scope for this ticket).

What is documented for the SSO/token endpoints: excessive **failed** requests (e.g. malformed
grants, repeated bad refresh attempts) trigger standard OAuth throttling — a `429 Too Many Requests`
response that must be respected via the `Retry-After` header — and CCP warns that **repeated
violations can result in an application being blocked outright**, requiring contacting support to
restore access. There's no published steady-state RPS/RPM figure for normal, successful token
exchanges/refreshes; the practical guidance is simply: don't hammer the endpoint, back off on 429s,
and don't do a full-scope re-login on every session when a stored refresh token still works (see the
scopeless/step-up login guidance in §3).

Source: [SSO Endpoint Deprecations blog post](https://developers.eveonline.com/blog/sso-endpoint-deprecations-2)

## Summary for implementation

For eve-trader's single-character, always-on backend:

1. Register one application, connection type "Authentication & API Access," callback URL pointing
   at the self-hosted host's OAuth callback route (HTTPS in production; `https://localhost/...` is
   fine only for local dev, must be swapped before deploy).
2. Request scope `esi-markets.read_character_orders.v1` up front (single-character, single-purpose
   tool — the "scopeless login then step up" pattern is aimed at multi-tenant apps with varied
   feature sets, and doesn't buy much here).
3. Do the standard authorization-code exchange once, store the returned `refresh_token` (encrypted
   at rest — it is effectively a long-lived credential), then refresh the access token via
   `grant_type=refresh_token` roughly every ~15–18 minutes (before the ~20-minute `expires_in`
   elapses) or lazily on 401s.
4. **Always overwrite the stored refresh token with whatever comes back from a refresh call** —
   rotation is expected, not just possible.
5. No SSO-specific rate-limit budgeting is needed for a single-character app doing one refresh
   every ~15–20 minutes; just handle 429/Retry-After defensively and avoid tight retry loops.
