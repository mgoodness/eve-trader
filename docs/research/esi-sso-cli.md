# EVE SSO / ESI OAuth for a local (headless) Go CLI — authoritative reference

Question: what is the correct, **current** OAuth2 flow for a local/headless Go
CLI authenticating against EVE Online's ESI/SSO — which flow, which scopes, how
do tokens live and rotate, what is the `X-Compatibility-Date` header, is the
access token a JWT, and what are the traps?

Written: **2026-09-30**. All live HTTP observations below were made on
2026-09-30 against production `login.eveonline.com` and
`esi.evetech.net` (tranquility). Every claim is tagged **[stated]** (present
verbatim in a primary source — cited) or **[inferred]** (my reasoning connecting
stated facts). Where a source is silent or two sources disagree it is called out
inline and collected in [§7](#7-disagreements-and-silences-between-sources) and
[§8](#8-open-questions--not-found).

Bottom line for the CLI:

- The flow is **OAuth 2.0 Authorization Code + PKCE (S256), no client secret**,
  against `https://login.eveonline.com/v2/oauth/authorize` and
  `https://login.eveonline.com/v2/oauth/token`. CCP's docs name this flow,
  expose it in the discovery document, and have told native apps to use it since
  2021. **[stated]**
- There is **no device-code flow** and no headless alternative. The user's
  browser must be opened at least once per character; after that the refresh
  token carries you forever. CCP's own tracker has an open, unimplemented
  request for RFC 8628 device flow dating from 2020. **[stated]**
- A **loopback `127.0.0.1` redirect is the right shape for a CLI, but CCP does
  not document `127.0.0.1` anywhere.** CCP requires the `redirect_uri` to match
  a registered callback exactly, and its only documented local examples are
  `https://localhost/callback/` and `http://localhost/oauth-callback`. Treat the
  loopback URI as a configuration value the user must register. **[stated +
  inferred]**
- For a **recommend-only v1** you need exactly two scopes for the stated
  requirement — `esi-skills.read_skills.v1` and
  `esi-characters.read_standings.v1` — plus the public market routes, which need
  no auth at all. Wallet and order scopes are needed **only if you actually call
  those endpoints**. **[stated]**
- **ESI has no write scope for market orders** — every order/wallet route is
  `GET`-only. "No execution" therefore costs you nothing in permissions; the
  scope exists only for reading. **[stated]**
- The access token **is a JWT**; the character id is the third field of the `sub`
  claim, `CHARACTER:EVE:<character-id>`. Access token life is **20 minutes**
  (`expires_in: 1200`); the authorization code lives **5 minutes**; the refresh
  token is long-lived and **may rotate on every refresh**, so always persist the
  refresh token the token endpoint returns. **[stated]**

---

## Method / sources

Primary sources used (fetched 2026-09-30 unless noted). "Legacy docs" means
`docs.esi.evetech.net`, which is CCP-operated but carries a banner saying it
"will no longer be updated" — it is still live and is quoted only where the
current docs are silent, and always marked as legacy.

| Source | URL / locator | Role |
|---|---|---|
| SSO docs (current) | <https://developers.eveonline.com/docs/services/sso/> | Flows, PKCE, JWT validation, claims |
| esi-docs repo (source of the site above) | `esi/esi-docs` @ `d377d955b9e508c1ce19b211bd3538dcbdc8abae` (2026-09-30), `docs/services/sso/index.md` | Verbatim doc text |
| esi-docs PKCE snippet | `esi/esi-docs` @ `d377d95`, `snippets/sso/authorization-code-pkce.py` | Exact PKCE byte handling |
| esi-docs JWT snippet | `esi/esi-docs` @ `d377d95`, `snippets/sso/validate-jwt-token.py` | Exact validation expectations |
| ESI docs — Overview | <https://developers.eveonline.com/docs/services/esi/overview/> | `X-Compatibility-Date` semantics |
| ESI docs — Best Practices | <https://developers.eveonline.com/docs/services/esi/best-practices/> | User-Agent policy, error limit, caching |
| ESI docs — Rate Limiting | <https://developers.eveonline.com/docs/services/esi/rate-limiting/> | Buckets, headers, token costs |
| **ESI OpenAPI spec (live)** | <https://esi.evetech.net/meta/openapi.json> (`openapi 3.1.0`, `info.version 2020-01-01`, `servers: https://esi.evetech.net`) | Endpoints, per-route scopes, `X-Compatibility-Date` parameter |
| **SSO discovery document (live)** | <https://login.eveonline.com/.well-known/oauth-authorization-server> | Endpoints, PKCE methods, auth methods |
| SSO JWKS (live) | <https://login.eveonline.com/oauth/jwks> | Signing keys/algorithms |
| ESI `/meta/compatibility-dates`, `/meta/changelog`, `/status` (live) | `https://esi.evetech.net/...` | Live compatibility-date list, echo header |
| Legacy docs — SSO Authorization Flow | <https://docs.esi.evetech.net/docs/sso/sso_authorization_flow.html> | Authorization-code and access-token **lifetimes** |
| Legacy docs — Refreshing tokens | <https://docs.esi.evetech.net/docs/sso/refreshing_access_tokens.html> | Refresh grant parameters, rotation warning, `expires_in: 1200` |
| Legacy docs — Native SSO flow | <https://docs.esi.evetech.net/docs/sso/native_sso_flow.html> | PKCE for desktop/mobile, callback semantics |
| Legacy docs — Creating an SSO Application | <https://docs.esi.evetech.net/docs/sso/creating_sso_application.html> | Callback URL rules, scope-change invalidates refresh tokens |
| Legacy docs — Validating JWT tokens | <https://docs.esi.evetech.net/docs/sso/validating_eve_jwt.html> | Full example JWT payload |
| Legacy docs — Migrate v1 → v2 | <https://docs.esi.evetech.net/docs/sso/migrate_v1_v2.html> | `/oauth/verify` → JWT claim mapping |
| Legacy ESI Swagger (archived) | Wayback `20211009225712id_/https://esi.evetech.net/latest/swagger.json` | Corp role annotations + cache TTLs that the current spec dropped |
| CCP blog — SSO Endpoint Deprecations (2021-10-07) | <https://developers.eveonline.com/blog/sso-endpoint-deprecations-2> | PKCE mandate for native apps, refresh-token rotation, v2 token format |
| CCP blog — Changing versions: `/v42/` (2025-07-10) | <https://developers.eveonline.com/blog/changing-versions-v42-was-getting-out-of-hand> | Compatibility dates, version-prefix migration |
| CCP blog — Removal of v1 authentication tokens (2025-04-11) | <https://developers.eveonline.com/blog/removal-of-v1-authentication-tokens> | JWT-only, `/v2` URLs |
| CCP blog — Deprecation and removal of unused scope (2025-05-22) | <https://developers.eveonline.com/blog/deprecation-and-removal-of-unused-scope-2025-06-11> | Scope rejection applies at **refresh** too |
| CCP blog — Hold your horses: rate limiting (2025-10-07) | <https://developers.eveonline.com/blog/hold-your-horses-introducing-rate-limiting-to-esi> | Token-cost model |
| CCP blog — Goodbye Swagger (2026-07-14) | <https://developers.eveonline.com/blog/goodbye-swagger-removing-the-last-remnants> | `swagger.json` removed 2026-08-11 |
| `ccpgames/sso-issues` (CCP's own SSO tracker) | <https://github.com/ccpgames/sso-issues> issues #13, #17, #31, #58, #60, #65, #66, #67 | Device flow status, PKCE verifier bug, callback-URL limit |
| `esi/esi-issues` (CCP's ESI tracker) | <https://github.com/esi/esi-issues> issues #1117, #1303 | "No browser" request, refresh token ≠ bearer token |
| Community Go SSO library (non-CCP) | `ErikKalkoken/eveauth`, listed in CCP's docs at <https://developers.eveonline.com/docs/community/eve-auth-for-go/> | Loopback-port practice only, cited as secondary |

`esi-docs` is CCP's official documentation repository (`docs/services/sso/index.md`
is owned by `@esi/ecm` per its `CODEOWNERS`), so quoting the markdown at a commit
is equivalent to quoting the rendered site and is more precisely citable.

---

## 1. The flow

### 1.1 Authorization Code + PKCE is the documented desktop/mobile flow **[stated]**

CCP's current docs describe exactly two flows: **Authorization Code** (for
"web applications that can securely store the client secret on the server side")
and **Authorization Code with PKCE**, which "is mostly aimed at mobile and
desktop applications that cannot securely store the client secret" —
`docs/services/sso/index.md` @ `d377d95`, lines for "### Authorization Code" and
"### Authorization Code with PKCE".

CCP has been explicit that this is not optional:

> "Native/mobile applications should use the PKCE flow as described here."
> — CCP blog, *SSO Endpoint Deprecations*, 2021-10-07

The discovery document confirms server-side support:

```json
{"issuer":"https://login.eveonline.com",
 "authorization_endpoint":"https://login.eveonline.com/v2/oauth/authorize",
 "token_endpoint":"https://login.eveonline.com/v2/oauth/token",
 "userinfo_endpoint":"https://login.eveonline.com/v2/oauth/verify",
 "response_types_supported":["code","token"],
 "jwks_uri":"https://login.eveonline.com/oauth/jwks",
 "revocation_endpoint":"https://login.eveonline.com/v2/oauth/revoke",
 "token_endpoint_auth_methods_supported":["client_secret_basic","client_secret_post","client_secret_jwt"],
 "code_challenge_methods_supported":["S256"]}
```

— <https://login.eveonline.com/.well-known/oauth-authorization-server>,
fetched 2026-09-30. (Note: `token_endpoint_auth_methods_supported` does **not**
list `none`; see [§7](#7-disagreements-and-silences-between-sources).)

The same endpoints appear in the ESI OpenAPI security scheme — the **only**
declared flow:

```json
"components": {"securitySchemes": {"OAuth2": {"type": "oauth2", "flows": {
  "authorizationCode": {
    "authorizationUrl": "https://login.eveonline.com/v2/oauth/authorize",
    "tokenUrl": "https://login.eveonline.com/v2/oauth/token",
    "scopes": { /* 72 entries */ }}}}}}
```

— <https://esi.evetech.net/meta/openapi.json>, fetched 2026-09-30. The spec
declares **no** `implicit`, `clientCredentials` or `deviceCode` flow, and no
`refreshUrl`.

### 1.2 Exact endpoints and parameters

**Authorization request** **[stated]** — `docs/services/sso/index.md` @
`d377d95`, "Authorization Code with PKCE" step 2:

| Parameter | Value |
|---|---|
| URL | `https://login.eveonline.com/v2/oauth/authorize` |
| `response_type` | `code` |
| `client_id` | your application's client id |
| `redirect_uri` | must match a registered redirect URL |
| `scope` | space-separated scope list |
| `state` | random string, **required by EVE SSO**, must be verified on return |
| `code_challenge` | `base64url(SHA-256(code_verifier))` |
| `code_challenge_method` | `S256` |

**Token request** **[stated]** — same file, step 3. `POST` to
`https://login.eveonline.com/v2/oauth/token` with
`Content-Type: application/x-www-form-urlencoded` and body:

```
grant_type=authorization_code
code=<authorization code>
code_verifier=<code verifier>
client_id=<your client id>
```

**No `Authorization: Basic` header in the PKCE flow** — the CCP snippet says
`# Note: we do not use the client secret in this flow`
(`snippets/sso/authorization-code-pkce.py` @ `d377d95`).

Two hard rules from CCP's 2021 blog (those are still the rules the server
enforces):

- "The `v2/oauth/token` endpoint will no longer accept data sent as a JSON
  payload or as a URL query. Data must be sent ... with
  `Content-Type: application/x-www-form-urlencoded`."
- "`code_challenge_method=S256` ... is the only method currently accepted."
  (legacy `native_sso_flow.html`)

### 1.3 The PKCE byte-handling that EVE actually accepts **[stated — read this before writing Go]**

CCP's PKCE example is **not** RFC 7636-conformant, and it is the version the
server accepts:

```python
code_verifier = base64.urlsafe_b64encode(secrets.token_bytes(32))
sha256 = hashlib.sha256()
sha256.update(code_verifier)
code_challenge = base64.urlsafe_b64encode(sha256.digest()).decode().rstrip("=")
```

— `snippets/sso/authorization-code-pkce.py` @ `d377d95`, function
`generate_code_challenge()`.

Read that carefully:

1. The verifier is **the base64url string of 32 random bytes**, not the raw
   bytes. `base64.urlsafe_b64encode` emits padding, so the verifier is 44
   characters and **ends in `=`**.
2. The challenge is `base64url(SHA-256(ASCII(verifier_string)))`, **unpadded**
   (`rstrip("=")`).
3. The token request sends that same base64url string as `code_verifier`.

RFC 7636 §4.1 restricts the verifier to `[A-Z] / [a-z] / [0-9] / "-" / "." /
"_" / "~"`, which excludes `=`; CCP's own issue tracker has this open as a bug
with no fix landed:

> "Current EVE SSO implementation of /v2/oauth/token endpoint for PKCE uses
> unnecessary BASE64URL-ENCODING for *code_verifier* parameter." ...
> "@stebet: This is the best issue report I've ever received! Thank you. I'll
> work on a fix :)"
> — <https://github.com/ccpgames/sso-issues/issues/60> (opened 2020-02-02, still
> **open**)

The same issue documents that raw verifiers of length 4n+1 characters produced
HTTP 500. **Follow the CCP snippet exactly**; do not "fix" it to be RFC-clean.
**[inferred recommendation, from the stated snippet + open issue]**

### 1.4 Device-code flow: does not exist **[stated]**

- The discovery document has **no** `device_authorization_endpoint` and **no**
  `grant_types_supported`; `response_types_supported` is `["code","token"]`.
- The current CCP docs describe no such flow.
- CCP's SSO tracker holds an open, unactioned request:
  > "Request to implentment OAuth 2.0 Device Flow ... This would be useful for
  > command line tools, headless tools on server processing pipelines and
  > libraries/runtimes that have no easy capability to run a browser or redirect
  > flow."
  > — <https://github.com/ccpgames/sso-issues/issues/65>, opened 2020-11-19,
  > still **open**, no CCP commitment in the thread.

**So a "headless" CLI means: one interactive browser round-trip per character,
then refresh tokens forever.** CCP states the same in its ESI tracker:
"SSO requires a browser to give consent at least once. Once you have your
initial authorization code, you can manually obtain your access/refresh_tokens.
Then just store the refresh_token in your app and use it."
— <https://github.com/esi/esi-issues/issues/1117#issuecomment-492855149>
(community maintainer comment in CCP's tracker; consistent with the docs).

### 1.5 The loopback redirect URI — what is and is not documented

**[stated]** The rule:

> "The application must define a redirect URL where the user is sent after
> completing the authorization flow. The redirect URL must be registered with
> the application. Any other URL will be rejected by the SSO service."
> — `docs/services/sso/index.md` @ `d377d95`, "Terms and important notes"
>
> "This URL must match one of the redirect URLs you registered with your
> application."
> — same file, both flows

**[stated]** The only local-development URIs CCP has ever written down:

> "You can start by using `https://localhost/callback/` during development (and
> is what the documentation in this repository uses). However, never use
> localhost as a callback URL for an application you have released. You can
> always edit the callback URL after you have defined it."
> — legacy docs, *Creating an SSO Application*
> <https://docs.esi.evetech.net/docs/sso/creating_sso_application.html>

> In the Callback URL field, enter the following URL: `http://localhost/oauth-callback`
> ... the EVE SSO will return an error if this does not match your clients
> designated callback
> — legacy CCP blog, *ESI Step by Step — SSO to Authenticated Calls*
> <https://developers.testeveonline.com/blog/article/sso-to-authenticated-calls>

**[inferred]** `http://127.0.0.1:<port>/callback` with a loopback listener
(socket bound to `127.0.0.1`) is the correct shape for a Go CLI, and both the
`http://` scheme and `localhost` host are demonstrably acceptable to the EVE SSO
(second quote above). But **CCP never names `127.0.0.1`, never states whether
the port is part of the match, and never states whether an ephemeral random port
would be accepted.** Two consequences worth encoding in the CLI:

- Make the redirect URI a **user-supplied configuration value** that the user
  pastes into the registration form, not a hard-coded literal.
- Prefer a **fixed port** and a fixed path. CCP's tracker has an open request
  for multiple callback URLs per application with no CCP reply
  (<https://github.com/ccpgames/sso-issues/issues/13> opened 2017-10-05;
  <https://github.com/ccpgames/sso-issues/issues/67> opened 2021-01-30), which
  **[inferred]** means one registered callback per app, i.e. a random port would
  break login. Verify in the portal before relying on it.

**[secondary, non-CCP, for reference only]** The Go community library CCP lists
in its docs — `ErikKalkoken/eveauth`, described as "A Go library for authorizing
desktop applications with the EVE Online SSO service" at
<https://developers.eveonline.com/docs/community/eve-auth-for-go/> — documents
its own registration as `http://localhost:8000/callback` and its Python sibling
as `http://127.0.0.1:8080/callback`. That is corroborating practice, not policy;
CCP does not own or vet those READMEs.

---

## 2. Scopes for a station-trading CLI

### 2.1 The exact strings

Scope strings are verbatim from
`https://esi.evetech.net/meta/openapi.json`
(`components.securitySchemes.OAuth2.flows.authorizationCode.scopes`, 72 entries,
fetched 2026-09-30) and from the per-route `security` arrays in the same
document.

| Route | Scope, exactly | Auth? |
|---|---|---|
| `GET /characters/{character_id}/skills` | `esi-skills.read_skills.v1` | required |
| `GET /characters/{character_id}/skillqueue` | `esi-skills.read_skillqueue.v1` | required |
| `GET /characters/{character_id}/standings` | `esi-characters.read_standings.v1` | required |
| `GET /characters/{character_id}/orders` | `esi-markets.read_character_orders.v1` | required |
| `GET /characters/{character_id}/orders/history` | `esi-markets.read_character_orders.v1` | required |
| `GET /characters/{character_id}/wallet` | `esi-wallet.read_character_wallet.v1` | required |
| `GET /characters/{character_id}/wallet/journal` | `esi-wallet.read_character_wallet.v1` | required |
| `GET /characters/{character_id}/wallet/transactions` | `esi-wallet.read_character_wallet.v1` | required |
| `GET /markets/{region_id}/orders` | *(none)* | public |
| `GET /markets/{region_id}/history` | *(none)* | public |
| `GET /markets/prices` | *(none)* | public |
| `GET /markets/{region_id}/types` | *(none)* | public |
| `GET /markets/structures/{structure_id}` | `esi-markets.structure_markets.v1` | required |
| `GET /corporations/{corporation_id}/orders` | `esi-markets.read_corporation_orders.v1` | required |
| `GET /corporations/{corporation_id}/standings` | `esi-corporations.read_standings.v1` | required |
| `GET /corporations/{corporation_id}/wallets` | `esi-wallet.read_corporation_wallets.v1` | required |

Note the exact spelling that trips people up: it is
`esi-markets.read_character_orders.v1` (plural **markets**, singular
*character*), not `esi-market.read_orders.v1` or `esi-markets.read_orders.v1`
(the latter exists but is the **corporation** variant,
`esi-markets.read_corporation_orders.v1`). The character scope string is
deliberately *not* symmetric with its corporation counterpart.

The character standings route returns "character standings from agents, NPC
corporations, and factions" — `openapi.json`, `description` of
`GET /characters/{character_id}/standings`. It is **not** a list of player-corp
relations.

### 2.2 What a recommend-only v1 actually needs

**[stated]** Per the OpenAPI `security` arrays above, scopes are required *per
route you call*. There is no "read-only" blanket scope and no scope that grants
market data.

**[inferred]** Therefore:

- The two scopes the brief names — `esi-skills.read_skills.v1` and
  `esi-characters.read_standings.v1` — are **sufficient** for a v1 that reads
  the character's skills and standings and recommends prices from the **public**
  region market routes (`GET /markets/{region_id}/orders` and
  `GET /markets/{region_id}/history` need no token).
- **Wallet and character-orders scopes are not needed by a v1 that only
  recommends.** Add `esi-markets.read_character_orders.v1` the moment you want
  to show what the pilot already has listed (e.g. to avoid recommending an item
  they are already selling, or to respect the order limit), and
  `esi-wallet.read_character_wallet.v1` the moment you want to budget against
  actual ISK rather than a user-entered number. Each addition is a real user
  consent prompt, so defer them.
- If you support Upwell (player structure) markets instead of NPC stations, you
  additionally need `esi-markets.structure_markets.v1`, and the route has no
  documented docking-access caveat in any first-party source (see
  [§8](#8-open-questions--not-found)).

### 2.3 There is no order-placement scope to avoid **[stated]**

Every order and wallet route in the spec is `GET`-only — verified over all 182
paths: `GET /characters/{character_id}/orders`,
`GET /characters/{character_id}/orders/history`,
`GET /corporations/{corporation_id}/orders`,
`/corporations/{corporation_id}/orders/history`, `/characters/{character_id}/wallet`
(+`/journal`, `/transactions`), `/corporations/{corporation_id}/wallets`
(+division journal/transactions) all expose only `get`.

The complete set of non-`read` scopes in the 72-entry list is
`esi-calendar.respond_calendar_events.v1`, `esi-characters.write_contacts.v1`,
`esi-corporations.track_members.v1`, `esi-fittings.write_fittings.v1`,
`esi-fleets.write_fleet.v1`, `esi-mail.organize_mail.v1`, `esi-mail.send_mail.v1`,
`esi-planets.manage_planets.v1`, `esi-ui.open_window.v1`,
`esi-ui.write_waypoint.v1` — **none of them touch the market.** A "recommends,
never executes" product needs no restraint here; ESI cannot place an order.

### 2.4 Registration gates the request, and changing it breaks tokens **[stated]**

> "Applications can also only request scopes that they have assigned in the
> application registration."
> — `docs/services/sso/index.md` @ `d377d95`

> "if you update the scopes needed for your application it will invalidate your
> refresh token and you will have to do the whole SSO flow from the beginning.
> This means making your users login again."
> — legacy docs, *Creating an SSO Application*

**[inferred]** Get the scope list right at registration. Enabling an extra scope
later is a breaking change for every stored refresh token, so register the v1
superset (`esi-skills.read_skills.v1`, `esi-characters.read_standings.v1`,
`esi-markets.read_character_orders.v1`, `esi-wallet.read_character_wallet.v1`)
even if v1 only *calls* the first two — the `scope=` parameter in the authorize
request can always be a subset of what the application is allowed to ask for.

---

## 3. Token lifetimes, refresh and rotation, and storage

### 3.1 Lifetimes **[stated — from CCP's legacy docs; the current docs are silent]**

| Artefact | Lifetime | Source |
|---|---|---|
| Authorization code | **5 minutes**, one use only | legacy *SSO Authorization Flow*; legacy *Native SSO flow* ("This authorization code is a one time use only token that has a lifetime of 5 minutes. If you do not respond within 5 minutes you will have to start over") |
| Access token | **20 minutes** | legacy *SSO Authorization Flow* ("an access token whose lifetime is 20 minutes"); `expires_in` observed as `1199` in the documented token response and `1200` in the documented refresh response |
| Refresh token | "lasts as long as the application does not revoke it"; "long-lived and can be used to obtain new access tokens indefinitely, as long as the user has not revoked the application's access" | legacy *SSO Authorization Flow*; current `docs/services/sso/index.md` |

Concretely, the documented token-endpoint response is:

```json
{"access_token":"<JWT>","expires_in":1199,"token_type":"Bearer","refresh_token":"<unique string>"}
```
— legacy docs, *Native SSO flow*

and the documented refresh response is:

```json
{"access_token":"MXP...tg2","token_type":"Bearer","expires_in":1200,"refresh_token":"gEy...fM0"}
```
— legacy docs, *Refreshing tokens*

The JWT example in the legacy *Validating JWT tokens* page has `exp` `1648563218`
and `iat` `1648562018` — a 1200-second gap. **[stated]**

**The current documentation states no duration at all.** It says only "The
access token is a time-limited token" and "The refresh token is long-lived".
Do not derive TTL from the current docs; use `expires_in` from the response and
the `exp` claim. **[stated]**

### 3.2 The refresh grant, exactly **[stated]**

From the legacy *Refreshing tokens* page, for a **native** application
(the web variant differs only in using `Basic` auth instead of `client_id`):

`POST https://login.eveonline.com/v2/oauth/token`, headers
`Content-Type: application/x-www-form-urlencoded` and `Host: login.eveonline.com`
(the sample shows no `Authorization` header for native), body:

```
grant_type=refresh_token
refresh_token=<refresh token>     # must be URL-encoded
client_id=<your client id>
scope=<subset of original scopes> # OPTIONAL; omit to get all original scopes
```

> "Remember that the refresh token must be URL-encoded, per the content type of
> the request. Failing to do this may cause the request to be malformed and a
> 400 response to be returned."

**Warning: the current SSO docs do not document the refresh grant at all.** The
new `docs/services/sso/index.md` has no refresh section; only narrative
sentences about refresh tokens. The parameter list above exists solely in the
legacy page. **[stated silence]**

Also: a `scope=` on the refresh is not free — see
<https://github.com/ccpgames/sso-issues/issues/77> ("Token refresh with a scope
parameter generate extremely long token"). **[inferred]** omit `scope` unless
you need to narrow permissions.

### 3.3 Rotation — persist whatever comes back **[stated]**

Three independent CCP statements:

> "Developers have until now been able to safely assume that their stored refresh
> tokens will never change. This will no longer be the case, so developers should
> assume that the refresh token MIGHT change when it is refreshed and update it
> when needed."
> — CCP blog, *SSO Endpoint Deprecations*, 2021-10-07

> "Please note that the refresh_token returned may not be the same as the refresh
> token submitted. At some point in the future the EVE SSO will enable refresh
> token rotation for native applications. Make sure to update the refresh token
> stored on the client side in those cases."
> — legacy docs, *Refreshing tokens*

> "as a native application you should expect the refresh token to be volatile,
> meaning you should always be prepared to receive a new refresh token every time
> you refresh your access tokens."
> — legacy docs, *Native SSO flow*

The v2 token shape is also stated: "the new refresh tokens apart from the old
ones as they are a 24 character strings (16 bytes Base64 encoded). V2 Refresh
token example: `MDEyMzQ1Njc4OWFiY2RlZg==`" — *SSO Endpoint Deprecations*. <!-- gitleaks:allow -->

**[inferred]** Treat the token response as the single source of truth: whenever
`refresh_token` is present and non-null in a 200 response from
`/v2/oauth/token`, atomically overwrite the stored value before using the new
access token. Never assume the old value survives.

### 3.4 Revocation **[stated]**

- Endpoint: `https://login.eveonline.com/v2/oauth/revoke` (discovery document).
  A POST with a bogus token returned **HTTP 200** live on 2026-09-30 — consistent
  with RFC 7009's "always 200" behaviour.
- Users can revoke third-party app access themselves: "Users can revoke access
  for individual apps on the support site" (legacy *Refreshing tokens*).
- A revoked refresh token surfaces as an error, not a silent failure. CCP's
  tracker notes the SSO historically returned `invalid_token` where OAuth would
  expect `invalid_grant`, "Some popular libraries like ScribeJava ... break on
  the EVE SSO response" — <https://github.com/ccpgames/sso-issues/issues/31>
  (opened 2018-04-27, **still open**). **[stated]**
- A refresh token is **not** a bearer credential: using one directly against
  ESI gives `403 {"error":"unexpected end of JSON input","sso_status":401}`
  — <https://github.com/esi/esi-issues/issues/1303>.

### 3.5 Storing the refresh token on a local machine

**CCP does not prescribe a storage mechanism.** No first-party source found
mentions OS keychains, file permissions, config directories, or encryption.
What CCP *does* state, and what a Go CLI must therefore satisfy:

- "The refresh token ... must be kept secure, as it can be used to obtain new
  access tokens." — current `docs/services/sso/index.md` @ `d377d95`
- "Store the refresh token somewhere secure and never share it." — legacy
  *Native SSO flow*
- "If your refresh token gets leaked it can only be invalidated by you, the
  application developer, by revoking it." — legacy *Native SSO flow*
- "**The `Secret Key` in particular needs to be stored securely and should never
  be shared**" — legacy *Web based SSO flow*. **[inferred]** With PKCE there is
  no secret to store, which removes this whole class of risk; do not register a
  secret-bearing flow you don't need.
- Changing registered scopes invalidates the token (§2.4), so any stored token
  must be recoverable-by-re-login rather than treated as permanent state.

**[inferred — engineering choice, not CCP policy]** Given the above constraints
and the absence of a stated mechanism: store one file per character under the
platform config dir (`os.UserConfigDir()` → `$XDG_CONFIG_HOME` / `~/Library/Application
Support` / `%AppData%`) with mode `0600`, or in the OS keychain
(macOS Keychain / libsecret / Windows Credential Manager) if you want at-rest
protection against other local users. Whatever you pick must (a) overwrite the
refresh token on every refresh (§3.3) and (b) be trivially deletable so a
revoked token can be cleared.

**Confirm your SSO app registration choice deliberately.** The legacy docs
describe an "Authentication Only" connection type that yields a token with
`"refresh_token": null` (observed response in
<https://github.com/ccpgames/sso-issues/issues/17>, and CCP's reply: "Currently
we don't issue Refresh tokens for auth-only requests (ones requesting no
scopes)"). Select **Authentication & API Access** when registering, or you will
never get a refresh token. **[stated]**

---

## 4. `X-Compatibility-Date`, versioning, and the JWT

### 4.1 The header **[stated]**

From <https://developers.eveonline.com/docs/services/esi/overview/>:

> "Every ESI request can include an `X-Compatibility-Date` header using the ISO
> format - `YYYY-MM-DD`. This header tells ESI, 'This application's ESI
> implementation was updated or reviewed at this date – give me the API behavior
> as it was at that date'. If applications cannot set custom headers, the
> `compatibility_date` query parameter will do the same. If a request does not
> set a compatibility date, the oldest available compatibility date is used."
>
> "The date cannot be in the future, neither can it below a minimum threshold
> (the 'oldest' versions available). If this minimum bar is raised, this will be
> clearly communicated via dev-blogs. The API changes date at 11:00 UTC."
>
> New compatibility date required for: adding routes; adding/changing
> (now-to-be) required request parameters; changing types of request params /
> response fields / response headers; removing request params; removing response
> fields / response headers / enum values. Existing dates absorb: adding optional
> request params; adding response fields / response headers / enum values.

Release policy from the blog that introduced it (2025-07-10): "As ESI endpoints
evolve, we will aim to maintain at least one year's backwards compatibility."

**Default for a new Go CLI [inferred]:** send the date your client was last
reviewed, and expose it as a constant you bump deliberately. Do not send
`now()` — the blog's own pseudocode note is that "today" is
`now() − 11h`, because the date rolls at 11:00 UTC; and sending a future date is
rejected.

**Available values, live** — `GET https://esi.evetech.net/meta/compatibility-dates`,
fetched 2026-09-30 (HTTP 200, `cache-control: public, max-age=600`):

```json
{"compatibility_dates":["2026-08-18","2026-08-04","2026-07-21","2026-07-17","2026-06-09",
"2026-05-19","2025-12-16","2025-11-06","2025-09-30","2025-09-26","2025-08-26","2025-04-02",
"2025-04-01","2020-01-01"]}
```

This list is authoritative and **more current than the OpenAPI spec's own
enum** (see [§7](#7-disagreements-and-silences-between-sources)).

**Response echo [stated, verified]:** the server returns an `X-Compatibility-Date`
response header; per the blog it "is always your requested date or earlier". Live
check on 2026-09-30: `GET /status` with `X-Compatibility-Date: 2025-01-01`
returned `x-compatibility-date: 2020-01-01`. The header is listed in
`access-control-expose-headers`, so browsers can read it too.

`GET /meta/changelog` returns the per-date change list (e.g.
`{"changelog":{"2020-01-01":[{"method":"GET","path":"/meta/changelog",...}]}}`) —
useful to decide when to bump.

### 4.2 Versioning and URLs **[stated]**

- The OpenAPI `servers` array is a bare `https://esi.evetech.net`. Call
  **unprefixed** paths: `https://esi.evetech.net/status`, not `/latest/status`.
- The migration blog instructs: "Remove all version prefixes from the paths you
  call (i.e. `/v1/status/` becomes `/status/`)" and notes "Existing versioned
  routes still work and will work for the foreseeable future."
- The Swagger artefacts are gone: "On Tuesday 11 August 2026, both
  `/_latest/swagger.json` and `/latest/swagger.json` will start returning a 404.
  From that moment on, OpenAPI is the only way to get the ESI specifications."
  Live-verified on 2026-09-30: `https://esi.evetech.net/_latest/swagger.json`
  → 404.
- SSO likewise has exactly one current base: everything is `/v2/oauth/*`. The
  v1 `oauth/*` endpoints were deprecated 2021-11-01 and "might be removed at any
  given time"; v1 *tokens* stopped being accepted 2025-05-13 ("the API Gateway
  will start validating tokens. From this moment on v1 tokens will no longer be
  accepted"). **[stated]** (Live check: `https://login.eveonline.com/oauth/verify`
  still answers 401 rather than 404, so the v1 path is still routed — do not use
  it.)

### 4.3 Is the access token a JWT? Yes. **[stated]**

> "The access token is a JWT (JSON Web Token) that contains information about the
> user and the scopes that have been granted."
> — current `docs/services/sso/index.md` @ `d377d95`, "Validating JWT Tokens"

Full example payload (legacy *Validating JWT tokens*):

```json
{
  "scp": ["esi-skills.read_skills.v1", "esi-skills.read_skillqueue.v1"],
  "jti": "998e12c7-3241-43c5-8355-2c48822e0a1b",
  "kid": "JWT-Signature-Key",
  "sub": "CHARACTER:EVE:123123",
  "azp": "my3rdpartyclientid",
  "tenant": "tranquility",
  "tier": "live",
  "region": "world",
  "aud": ["my3rdpartyclientid", "EVE Online"],
  "name": "Some Bloke",
  "owner": "8PmzCeTKb4VFUDrHLc/AeZXDSWM=",
  "exp": 1648563218,
  "iat": 1648562018,
  "iss": "login.eveonline.com"
}
```

Claim roles **[stated]**:

| Claim | Meaning |
|---|---|
| `sub` | `CHARACTER:EVE:<character-id>` — "can be used to get the current character's ID" (current docs) |
| `name` | character name |
| `scp` | array of granted scopes |
| `aud` | array containing your `client_id` **and** the literal `"EVE Online"`; both must be checked |
| `exp` / `iat` | Unix timestamps; `exp − iat = 1200` in the example |
| `iss` | either `login.eveonline.com` or `https://login.eveonline.com/` |
| `owner` | CharacterOwnerHash — changes if the character changes hands |
| `azp` | client id of the requestor |
| `jti`, `tenant`, `tier`, `region`, `kid` | identifier / tenancy metadata |

**Getting the character id**: `strings.Split(sub, ":")[2]`, or equivalently the
legacy mapping table which shows the whole v1→v2 field correspondence:

```python
{"CharacterID": data["sub"].split(":")[2], "CharacterName": data["name"],
 "ExpiresOn": datetime.fromtimestamp(data["exp"]).isoformat(), "Scopes": data["scp"],
 "TokenType": "JWT", "CharacterOwnerHash": data["owner"], "ClientID": data["azp"]}
```
— legacy docs, *Migrate from v1 to v2*

**The `sub` format was itself a documentation bug that got fixed in the
docs — not in the tokens.** Commit `e828db24fa6f6fb793c23c269f114d8b9765e374`
("Fix JWT token 'sub' claim format (#266)") changed the current doc from
`EVE:CHARACTER:<character-id>` to `CHARACTER:EVE:<character-id>`, with the
message: "The actual token returned by The SSO endpoint and used by
Applications, including the official API Explorer ... has this format."
Older copies of the CCP docs still in the wild say `EVE:CHARACTER:`. Parse the
observed format (`CHARACTER:EVE:`) but **split on `:` and take index 2**
rather than matching a prefix, so either ordering works. **[inferred]**

**Signing** **[stated]**: current docs say the JWT is "signed by the EVE SSO
using an RSA key" and to fetch the key from the JWKS URI in the discovery
document. The legacy page adds: "Currently the SSO uses the RS-256 signature
method, but will also support ES-256 in the near future." That future has
arrived — `https://login.eveonline.com/oauth/jwks` on 2026-09-30 returns:

```json
{"keys":[{"alg":"RS256","e":"AQAB","kid":"JWT-Signature-Key","kty":"RSA","use":"sig"},
         {"alg":"ES256","crv":"P-256","kid":"8878a23f-2489-4045-989e-4d2f3ec1ae1a","kty":"EC","use":"sig"}]}
```

**[inferred]** Select the key by `kid` **and** `alg` from the token header (which
is exactly what CCP's snippet does), and never hard-code `RS256`. Note the
discovery document's `id_token_signing_alg_values_supported` is `["HS256"]`,
which is *not* the access-token algorithm — do not read that field as the
access-token signing algorithm.

### 4.4 `/v2/oauth/verify` (userinfo) — advertised, but prefer the JWT **[stated]**

The discovery document still advertises
`"userinfo_endpoint": "https://login.eveonline.com/v2/oauth/verify"`. Live probe
on 2026-09-30 with a bogus bearer returned **HTTP 401** (so the route exists), and
the v1 path `/oauth/verify` also returned 401.

But CCP deprecated verify in favour of local JWT validation:

> "The `oauth/verify` endpoint will also be deprecated, since the v2 endpoints
> return a JWT token, enabling your applications to validate the JWT tokens
> without having to make a request to the SSO for each token."
> — CCP blog, *SSO Endpoint Deprecations*, 2021-10-07

**[inferred]** For a CLI, validate locally and take the character id from `sub`.
Do not add a `/v2/oauth/verify` round-trip; it is documented nowhere in the
current docs and its predecessor was explicitly deprecated.

---

## 5. Character vs corporation endpoints, and standing caveats

**Path scoping [stated]**: character data lives under `/characters/{character_id}/…`
and is gated by `esi-characters.*` / `esi-skills.*` / `esi-markets.read_character_orders.v1`
/ `esi-wallet.read_character_wallet.v1`; corporation data lives under
`/corporations/{corporation_id}/…` and is gated by `esi-corporations.*` /
`esi-markets.read_corporation_orders.v1` / `esi-wallet.read_corporation_wallets.v1`
— all read off `<https://esi.evetech.net/meta/openapi.json>`.

**The `{character_id}` in the path is not free** — it must be the character the
token was issued for. The docs state the token "is valid only for the character
and scopes that the user has consented to" (`docs/services/sso/index.md`).
**[inferred]** Derive it from `sub` and never ask the user for it; a mismatch is
a 403.

**Corporation routes additionally require an in-game corporate role.** The
current OpenAPI **drops this information**, so the role requirements can only be
cited from the archived Swagger:

| Route | Archived annotation |
|---|---|
| `GET /corporations/{corporation_id}/orders/` | "Requires one of the following EVE corporation role(s): Accountant, Trader" |
| `GET /corporations/{corporation_id}/orders/history/` | "Requires one of the following EVE corporation role(s): Accountant, Trader" |
| `GET /corporations/{corporation_id}/wallets/` (+journal, transactions) | "Requires one of the following EVE corporation role(s): Accountant, Junior_Accountant" |
| `GET /corporations/{corporation_id}/standings/` | *(no role annotation)* |
| 25 other corporation routes | Director / Factory_Manager / Station_Manager, etc. |

— archived ESI Swagger, snapshot `20211009225712` (2021-10-09),
`https://web.archive.org/web/20211009225712id_/https://esi.evetech.net/latest/swagger.json`,
`description` fields.

**Confirmed documentation regression:** the phrase "Requires one of the
following" occurs **28 times** in that 2021 Swagger and **zero times** in the
live 2026 OpenAPI. **[stated — counted both documents on 2026-09-30]** So a
trading CLI that offers corporation wallets/orders must communicate the role
requirement itself; the spec will not tell it.

**Caching differences that matter to a recommender [stated]** (legacy Swagger
`description` lines, current spec relies on the same `expires`/`max-age`
behaviour):

| Route | Archive-stated cache | Live rate-limit group (`x-rate-limit` in current OpenAPI) |
|---|---|---|
| `GET /characters/{character_id}/standings` | 3600 s | `char-social`, 600 tokens / 15 min |
| `GET /corporations/{corporation_id}/standings` | 3600 s | `corp-member`, 300 tokens / 15 min |
| `GET /characters/{character_id}/skills` | 120 s | `char-detail`, 600 tokens / 15 min |
| `GET /characters/{character_id}/skillqueue` | — | `char-detail`, 600 tokens / 15 min |
| `GET /characters/{character_id}/wallet` | — | `char-wallet`, 150 tokens / 15 min |
| `GET /characters/{character_id}/orders` | 1200 s | *(none declared)* |
| `GET /corporations/{corporation_id}/orders` | 1200 s | *(none declared)* |
| `GET /corporations/{corporation_id}/wallets` | 300 s | `corp-wallet`, 300 tokens / 15 min |
| `GET /markets/{region_id}/orders` | 300 s | `market-order`, **12000 tokens / 15 min** |
| `GET /markets/structures/{structure_id}` | 300 s | *(none declared)* |

**Standings caveat [stated]**: the route returns standings toward *agents, NPC
corporations and factions* — the numbers that reduce NPC-station broker fees.
It is not a corporate-relations feed, and there is no ESI route that returns a
station's owner corp for fee purposes (the sibling research note
`docs/research/eve-market-mechanics-and-esi.md` §6.5 covers the adjacent
character endpoints).

**[inferred]** For v1, stay character-only: `/characters/{character_id}/standings`
+ `/characters/{character_id}/skills` + public region market. Corporation
endpoints add a role dependency, a second scope family, and a slower cache for
signal that a station-trading recommender at one NPC station does not need.

---

## 6. Common pitfalls, in the order they bite

1. **Redirect URI must be registered and must match exactly.** "The redirect URL
   must be registered with the application. Any other URL will be rejected by
   the SSO service." — current docs. **[stated]** And the portal appears to allow
   **one** callback URL per application (open requests
   <https://github.com/ccpgames/sso-issues/issues/13>,
   <https://github.com/ccpgames/sso-issues/issues/67>, neither answered by CCP).
   Use a fixed port and make the URI configurable.

2. **`http://localhost` is fine; `127.0.0.1` is undocumented.** CCP's legacy blog
   registered `http://localhost/oauth-callback`; its legacy application guide
   says `https://localhost/callback/`; no CCP source names `127.0.0.1`. **[stated]**

3. **The token endpoint accepts only form-encoded bodies.** JSON or querystring
   bodies stopped working in November 2021. "Data must be sent to the endpoint
   with `Content-Type: application/x-www-form-urlencoded`." — *SSO Endpoint
   Deprecations*. **[stated]**

4. **The PKCE verifier is not the RFC shape.** Base64url-encode 32 random bytes,
   send *that padded string* as `code_verifier`, and hash *that string* for the
   challenge. Open bug: <https://github.com/ccpgames/sso-issues/issues/60>.
   **[stated]**

5. **`state` is required by the EVE SSO**, not merely recommended, and must be
   verified on the callback. **[stated]**

6. **Scope rejection happens at authorize *and* at refresh.** Requesting a scope
   your application has not enabled is rejected, and so is a scope CCP has
   removed: "This scope already is not used by any endpoint, but applications
   requesting this scope during authentication **or token refresh** will be
   rejected by the SSO from that date onwards." — CCP blog, *Deprecation and
   removal of unused scope*, 2025-05-22. **[stated]** Corollary: never hard-code
   an "all scopes" request.

7. **Changing the app's registered scopes invalidates every stored refresh
   token** (§2.4). Tell the user to expect to log in again. **[stated]**

8. **Do not treat the refresh token as a bearer token.** Sending it to ESI
   returns `403 {"error":"unexpected end of JSON input","sso_status":401}`
   — <https://github.com/esi/esi-issues/issues/1303>. **[stated]**

9. **Refresh proactively and persist rotation.** Refresh when `exp`
   (or `issued_at + expires_in`) is within a safety window; overwrite the stored
   refresh token on every successful response (§3.3). **[stated]** A revoked
   token may surface as `invalid_token` rather than `invalid_grant`, which some
   OAuth libraries misinterpret — <https://github.com/ccpgames/sso-issues/issues/31>.
   **[stated]**

10. **Expired/deformed tokens**: the SSO has historically issued tokens whose
    `exp` was absurdly far in the future, and ESI rejected them with
    `403 {"error":"token expiry is too far in the future","sso_status":200}`
    — <https://github.com/ccpgames/sso-issues/issues/66>. **[stated]** Treat
    that as "refresh again", not as a scope/permission bug.

11. **Identify your application on every ESI request.** CCP's Best Practices
    page: "All ESI requests **should** contain User Agent information indicating
    the application making the request ... Not abiding to the information
    transmitted can lead to your app being **banned**, for various time, from
    accessing the resources." Send `User-Agent` (a Go CLI can), formatted
    *narrow to broad*:
    ```
    AppName/1.2.3 (foo@example.com; +https://github.com/your/repository) LibraryName/1.2.3
    ```
    Preferred contents, in CCP's order: an email address (**strongly
    preferred**), app name with version (**strongly preferred**), a URL to
    source code, a Discord username, an EVE character. Browsers cannot set
    `User-Agent`, so CCP offers `X-User-Agent`, and `user_agent` as a query
    parameter of last resort. **[stated]** — note the OpenAPI spec contains **no**
    `user_agent` parameter (0 occurrences on 2026-09-30), so this is a
    behavioural convention, not a spec artefact. **[stated]**

12. **Two overlapping rate limiters, two disjoint header sets.** The floating
    bucket limiter returns `X-Ratelimit-Group/Limit/Remaining/Used` (and
    `Retry-After` on 429); the older **error** limiter allows "at most 100
    non-2xx/3xx responses per minute. After that, it will return 420s on all ESI
    routes" and uses `X-ESI-Error-Limit-Remain` / `X-ESI-Error-Limit-Reset`.
    "These headers are mutually exclusive." Discovered counts cost 5 tokens, so a
    retry loop that 403s is doubly expensive. **[stated]**

13. **Request headers are a documented, live API surface.** The live
    `access-control-expose-headers` on `/meta/compatibility-dates` is
    `Etag, Retry-After, X-Compatibility-Date, X-Esi-Error-Limit-Remain, X-Esi-Error-Limit-Reset, X-Pages, X-Ratelimit-Group, X-Ratelimit-Limit, X-Ratelimit-Remaining, X-Ratelimit-Used`.

14. **Do not copy CCP's JWT-validation snippet verbatim.** The issuer tuple is
    wrong:
    ```python
    ACCEPTED_ISSUERS = ("logineveonline.com", "https://login.eveonline.com")
    ```
    — `snippets/sso/validate-jwt-token.py` @ `d377d95` (commit
    `40c47d7`, unchanged since). `logineveonline.com` is missing a dot — the
    prose above it correctly says the issuer "should be
    `https://login.eveonline.com/`" and "`login.eveonline.com` may be used".
    Accept `login.eveonline.com` (the observed value) and
    `https://login.eveonline.com` (and, if you want to be generous, the
    trailing-slash form), and reject everything else. **[stated — the prose and
    the code contradict each other]**

---

## 7. Disagreements and silences between sources

1. **`X-Compatibility-Date` is documented as optional, but the OpenAPI spec marks
   it `required: true` and pins a single-value enum.**
   ```json
   "CompatibilityDate": {"description": "The compatibility date for the request.",
     "in": "header", "name": "X-Compatibility-Date", "required": true,
     "schema": {"enum": ["2020-01-01"], "format": "date", "type": "string"}}
   ```
   — `components.parameters.CompatibilityDate` in
   <https://esi.evetech.net/meta/openapi.json>. The ESI docs say "Every ESI
   request **can** include" the header and "If a request does not set a
   compatibility date, the oldest available compatibility date is used."
   Live behaviour agrees with the docs, not the spec: `GET /status` **without**
   the header returned **HTTP 200** with `x-compatibility-date: 2020-01-01`
   (2026-09-30). And the live `/meta/compatibility-dates` list has 14 values,
   versus the spec's 14-month-stale enum of one. **Winner: the docs + live
   behaviour for optionality; `/meta/compatibility-dates` for the valid set.**
   Read the enum as a generator artefact, not a constraint. **[stated]**

2. **Current docs vs legacy docs on token lifetimes.** Current SSO docs state no
   duration; legacy CCP docs state 5 min / 20 min and give `expires_in` values.
   Both are CCP-owned. **Winner: the legacy numbers**, corroborated by the JWT
   `exp − iat = 1200` example and the token response bodies; but treat
   `expires_in`/`exp` in each actual response as authoritative. **[stated]**

3. **Current docs vs legacy docs on the refresh grant.** The current SSO page
   documents no refresh request at all; the legacy page documents the exact
   parameters. **Winner: legacy page** — and it is the only first-party
   parameter reference that exists. **[stated]**

4. **Corporation role requirements vanished from the spec.** 28 routes carried
   "Requires one of the following EVE corporation role(s)…" in the 2021-10-09
   Swagger; the live 2026 OpenAPI contains the phrase 0 times. Neither the
   ESI docs nor the ESI overview mention corporate roles. **Winner: the archived
   Swagger**, since no source contradicts it and role gating is upstream game
   logic. **[stated + inferred]**

5. **`token_endpoint_auth_methods_supported` omits `none`, yet the documented
   PKCE flow uses no client authentication.** The discovery document lists only
   `client_secret_basic`, `client_secret_post`, `client_secret_jwt`. However,
   CCP's own PKCE snippet posts `client_id` in the body with **no**
   `Authorization` header and explicitly says "we do not use the client secret
   in this flow". **Winner: CCP's snippet** (it is presented as working), but
   flag it: a strict OIDC client that insists on seeing `none` advertised may
   refuse the flow. **[stated + inferred]**

6. **`id_token_signing_alg_values_supported: ["HS256"]` vs the RS256/ES256 JWKS.**
   The discovery document advertises HS256 for id tokens while the actual
   access-token signing keys are RS256 and ES256. Nothing in the
   discovery document describes the access-token algorithm. **[stated]**

7. **`iss` claim form.** Current docs: "should be `https://login.eveonline.com/`.
   In some cases, `login.eveonline.com` may be used." Legacy example payload:
   `"iss": "login.eveonline.com"`. The snippet's issuer tuple is misspelled
   (§6.14). **Winner: accept both correct forms; ignore the snippet's typo.**
   **[stated]**

8. **CCP's own template text is stale in two places.** The legacy
   `native_sso_flow.html` line "`redirect_uri=` — Replace all text after the `=`
   with the client ID" is a copy-paste error from the web-flow template (the
   bullet immediately below it in the fixed variant says to use the full callback
   URL). **[stated]**

---

## 8. Open questions / not found

- **Loopback port-matching rules.** No first-party source states whether the
  registered callback's port must match, whether an ephemeral port is allowed, or
  whether `127.0.0.1` is acceptable at all. Not stated anywhere in
  `esi-docs`, the ESI docs site, the SSO docs site, or the discovery document.
  Verify by registering and probing.
- **Whether the developers portal still permits only one callback URL.** Two
  open CCP-tracker requests imply yes, but no CCP statement or documentation
  confirms it. Not found.
- **Whether refresh-token rotation is *live*.** CCP says the token "MIGHT change"
  and to "expect the refresh token to be volatile", and the legacy page says
  rotation will be enabled "at some point in the future". No source states that
  it is on today. Behaviour is therefore **indeterminate by documentation**; the
  only safe implementation is to always store what comes back.
- **A first-party statement of the access-token TTL in the current docs.** Not
  found; only the legacy pages.
- **Structure-market docking/ACL gate.** No first-party document states that
  reading `GET /markets/structures/{structure_id}` requires docking access to the
  structure. The requirement is asserted only in community venues
  (<https://github.com/ccpgames/sso-issues/issues/23> is about the *scope*, and
  is about the general "structure information" behaviour, not this route).
  This duplicates the open question in
  `docs/research/eve-market-mechanics-and-esi.md` §"Open questions".
- **Any CCP guidance on local refresh-token storage mechanics** (keychain,
  file permissions, encryption). Not found — CCP states "store it securely" and
  nothing more.
- **`esi.activity.char:read` / `esi.cosmetic.char:read`** appear in the
  OpenAPI scope dictionary but I found **no route** whose `security` references
  them (grepped all 182 paths). Possibly used by non-ESI surfaces. Not found;
  do not request them.
- **A published Go SDK from CCP.** None. CCP's docs list only community
  libraries (`eve-auth-for-go` etc.) in its community showcase.

---

## Appendix — the minimal correct sequence for the CLI

```
1. Register an app at https://developers.eveonline.com/applications
   - Connection type: Authentication & API Access   (NOT "Authentication Only" — no refresh token)
   - Callback URL: <something the user controls, e.g. http://localhost:<fixed-port>/callback>
   - Scopes: esi-skills.read_skills.v1, esi-characters.read_standings.v1
             (+ esi-markets.read_character_orders.v1, esi-wallet.read_character_wallet.v1 if used)
   - Copy the client_id. There is no secret to store.                        [stated §2.4, §3.5]

2. Bind a loopback listener, generate:
   verifier  = base64url(32 random bytes)                     # PADDED, per CCP  [stated §1.3]
   challenge = base64url(SHA-256(verifier))  with "=" stripped
   state     = random

3. Open the user's browser at
   https://login.eveonline.com/v2/oauth/authorize
     ?response_type=code
     &client_id=<id>
     &redirect_uri=<urlencoded registered callback>
     &scope=<urlencoded space-separated scopes>
     &state=<state>
     &code_challenge=<challenge>&code_challenge_method=S256                  [stated §1.2]

4. On the callback, verify state, take code. Code expires in 5 minutes.

5. POST https://login.eveonline.com/v2/oauth/token
   Content-Type: application/x-www-form-urlencoded      (no Authorization header)
     grant_type=authorization_code&code=<code>&client_id=<id>&code_verifier=<verifier>
   -> {access_token (JWT), token_type: Bearer, expires_in: 1200, refresh_token} [stated §1.2, §3.1]

6. Validate the JWT locally: signature via kid+alg from
   https://login.eveonline.com/oauth/jwks; iss in {login.eveonline.com,
   https://login.eveonline.com}; aud contains <client_id> and "EVE Online";
   exp not passed. Character id = sub.split(":")[2].                      [stated §4.3]

7. Call ESI with:
   Authorization: Bearer <access_token>
   X-Compatibility-Date: <your review date>
   User-Agent: EveTrader/0.x (<email>; +https://github.com/mgoodness/eve-trader)
   GET https://esi.evetech.net/characters/{id}/skills      (esi-skills.read_skills.v1)
   GET https://esi.evetech.net/characters/{id}/standings   (esi-characters.read_standings.v1)
   GET https://esi.evetech.net/markets/10000030/orders     (public)        [stated §2.1, §4.1, §6.11]

8. On expiry (or ~1 minute before exp), POST /v2/oauth/token with
   grant_type=refresh_token&refresh_token=<stored>&client_id=<id>
   and OVERWRITE the stored refresh token with whatever comes back.       [stated §3.2, §3.3]
```
