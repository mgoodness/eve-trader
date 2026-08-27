# Research: ESI market & character-order endpoint limits and data shapes

Resolves: [#7](https://github.com/mgoodness/eve-trader/issues/7) (child of [#1](https://github.com/mgoodness/eve-trader/issues/1))

Researched 2026-08-27 against primary sources only:

- ESI OpenAPI spec, fetched live from `https://esi.evetech.net/meta/openapi.json` (title "EVE SKINR Ingenuity (ESI) - tranquility", spec version `2020-01-01`)
- Live response headers from `https://esi.evetech.net/latest/...` calls
- `https://developers.eveonline.com/docs/services/esi/best-practices/`
- `https://developers.eveonline.com/docs/services/esi/rate-limiting/`
- `https://docs.esi.evetech.net/docs/esi_introduction.html`

---

## 1. `GET /characters/{character_id}/orders/`

Source: `paths./characters/{character_id}/orders.get` in the OpenAPI spec.

- **Summary**: "List open orders from a character"
- **Description**: "List open market orders placed by a character"
- **Required scope**: `esi-markets.read_character_orders.v1` (from `security[0].OAuth2`)
- **Cache duration**: `x-cache-age` / `x-server-cache-ttl` / `x-client-cache-ttl` = **1200 seconds (20 minutes)**
- **Cache mode**: `x-server-cache-mode: ttl-based`
- **Parameters**: `character_id` (path, required), plus standard `Accept-Language`, `If-None-Match`, `If-Modified-Since`, `Compatibility-Date`, `Tenant` headers/params
- **Response**: JSON array of order objects (`CharactersCharacterIdOrdersGet` schema), one entry per open order:
  - `order_id` (int64), `type_id` (int64), `region_id` (int64), `location_id` (int64, station/structure the order sits at)
  - `is_buy_order` (bool), `is_corporation` (bool)
  - `price` (double), `volume_total` (int64), `volume_remain` (int64), `min_volume` (int64)
  - `duration` (int64, days), `issued` (date-time), `escrow` (double, buy orders only)
  - `range` (enum: `station`, `solarsystem`, `region`, or a jump-count string `"1"`…`"40"`)
  - No pagination (`X-Pages` header not defined for this route) — a character's open-order list is returned in one page. (ESI's UI caps a character at ~2000 concurrent orders regardless.)

## 2. `GET /characters/{character_id}/orders/history/`

Source: `paths./characters/{character_id}/orders/history.get` in the OpenAPI spec.

- **Summary**: "List historical orders by a character"
- **Description**: "List cancelled and expired market orders placed by a character **up to 90 days in the past**." — the 90-day retention window is stated verbatim in the spec description.
- **Required scope**: `esi-markets.read_character_orders.v1` (same scope as the open-orders endpoint — one scope grant covers both)
- **Cache duration**: `x-cache-age` / `x-server-cache-ttl` / `x-client-cache-ttl` = **3600 seconds (1 hour)**
- **Cache mode**: `ttl-based`
- **Parameters**: `character_id` (path, required), `page` (query, optional, int32, min 1)
- **Pagination**: paginated — response carries an `X-Pages` header (int64, total page count)
- **Response**: JSON array of order objects (`CharactersCharacterIdOrdersHistoryGet` schema) — same fields as the open-orders schema, plus:
  - `state` (enum: `cancelled`, `expired`) — the terminal state of the order

## 3. `GET /markets/{region_id}/history/`

Source: `paths./markets/{region_id}/history.get` in the OpenAPI spec, cross-checked with a live call.

- **Summary**: "List historical market statistics in a region"
- **Description**: "Return a list of historical market statistics for the specified type in a region. **This route expires daily at 11:05**" (UTC) — stated verbatim in the spec description; no `x-cache-age`/`x-server-cache-ttl` field is set because expiry is a fixed wall-clock time rather than a rolling TTL.
- **Live confirmation**: `curl -sD - "https://esi.evetech.net/latest/markets/10000002/history/?datasource=tranquility&type_id=34"` on 2026-08-27 returned `expires: Fri, 28 Aug 2026 11:05:00 GMT` and `last-modified: Thu, 27 Aug 2026 11:10:42 GMT` — confirms the daily-at-11:05-UTC expiry.
- **No auth required** (public endpoint, no `security` block)
- **Scope**: per-region only — there is no station/structure-level history endpoint. Parameters: `region_id` (path, required), `type_id` (query, required) — one item type per call.
- **Response**: JSON array of `MarketsRegionIdHistoryGet` entries, one per day:
  - `date` (date), `order_count` (int64), `volume` (int64, total units traded that day)
  - `highest` (double), `lowest` (double), `average` (double)

## 4. `GET /markets/{region_id}/orders/`

Source: `paths./markets/{region_id}/orders.get` in the OpenAPI spec, cross-checked with a live call.

- **Summary**: "List orders in a region"
- **Cache duration**: `x-cache-age` / `x-server-cache-ttl` / `x-client-cache-ttl` = **300 seconds (5 minutes)**
- **Live confirmation**: a call to `https://esi.evetech.net/latest/markets/10000002/orders/` returned `last-modified: ...19:27:37 GMT` and `expires: ...19:32:37 GMT` — exactly 300 seconds apart.
- **No auth required** (public endpoint)
- **Rate-limit group**: this route (uniquely among the ones researched here) carries an explicit `x-rate-limit` block: `group: market-order`, `max-tokens: 12000`, `window-size: 15m`. Confirmed live via response headers: `x-ratelimit-group: market-order`, `x-ratelimit-limit: 12000/15m`, `x-ratelimit-remaining`, `x-ratelimit-used`.
- **Parameters**: `region_id` (path, required), `order_type` (query, required, enum `buy`/`sell`/`all`, default `all`), `type_id` (query, optional — omit to get all items in the region), `page` (query, optional)
- **Pagination**: paginated, `X-Pages` header present. Live call to `/markets/10000002/orders/?order_type=all&page=1` (all items, The Forge) returned **`x-pages: 411`** — i.e. the full region-wide order book for Jita's region is ~411 pages.
- **Scope: per-region, not per-station/structure.** The route returns every open order anywhere in the given region — Jita, Perimeter, and every other system in The Forge, all mixed together. There is no way to ask ESI for "just this station's" order book directly.
- **Response**: JSON array of `MarketsRegionIdOrdersGet` entries:
  - `order_id`, `type_id`, `location_id` (station/structure ID — **this is the field to filter on for station-level views**), `system_id` (solar system ID)
  - `is_buy_order`, `price`, `volume_total`, `volume_remain`, `min_volume`, `duration`, `issued`, `range`
  - Note: unlike the character-orders schema, this schema has **no `region_id` field** (redundant with the path param) and **no `escrow` field**.

## 5. Region IDs and trade-hub stations

Confirmed live against ESI's `/universe/regions/{region_id}/`, `/universe/systems/{system_id}/`, and `/universe/stations/{station_id}/` endpoints.

| Hub  | Region       | region_id  | System | system_id | Primary trade-hub station                              | station_id |
|------|--------------|------------|--------|-----------|----------------------------------------------------------|------------|
| Jita | The Forge    | 10000002   | Jita   | 30000142  | Jita IV - Moon 4 - Caldari Navy Assembly Plant           | 60003760   |
| Rens | Heimatar     | 10000030   | Rens   | 30002510  | Rens VI - Moon 8 - Brutor Tribe Treasury                 | 60004588   |

`GET /universe/regions/10000002/` → `"name": "The Forge"`; `GET /universe/regions/10000030/` → `"name": "Heimatar"`. `GET /universe/systems/30000142/` lists 18 stations in the `stations` array, of which `60003760` resolves (via `GET /universe/stations/60003760/`) to `"Jita IV - Moon 4 - Caldari Navy Assembly Plant"`. `GET /universe/systems/30002510/` lists 8 stations, of which `60004588` resolves to `"Rens VI - Moon 8 - Brutor Tribe Treasury"`.

**Implication for the scanner**: yes, real trading for both hubs concentrates at one specific station each, not spread evenly across the region. Since `/markets/{region_id}/orders/` returns the whole region undifferentiated, the tool must fetch the full regional order book (all ~411 pages for The Forge) and filter client-side on `location_id == 60003760` (Jita) / `60004588` (Rens) to get hub-specific order books. There is no cheaper per-station ESI query for this — station/structure-scoped order books only exist for player-owned structures (`GET /markets/structures/{structure_id}/`, auth-gated, not relevant here since NPC stations aren't "structures").

## 6. ESI rate limits and caching model

Sources: `developers.eveonline.com/docs/services/esi/best-practices/`, `developers.eveonline.com/docs/services/esi/rate-limiting/`, live response headers.

**Caching model (applies to all endpoints)**:
- Every response carries `Expires` and `Last-Modified` headers. `Expires` tells the client exactly when new data will be available server-side; **clients should not re-request before that time** — ESI serves a cached copy anyway (`x-esi-cache-status: HIT` was observed on both live test calls), so polling faster than `Expires` wastes request budget for no new data.
- `ETag` is also returned; pair with `If-None-Match` (or `Last-Modified` with `If-Modified-Since`) for conditional requests that can return `304 Not Modified`.
- The per-endpoint cache windows found above (1200s / 3600s / 300s / daily-at-11:05-UTC) are the effective minimum poll intervals for each route.

**Error limit** (general, legacy, applies to *all* routes):
- Headers `X-Esi-Error-Limit-Remain` and `X-Esi-Error-Limit-Reset` (both observed live, e.g. `x-esi-error-limit-remain: 98`, `x-esi-error-limit-reset: 32`).
- Budget: **at most 100 non-2xx/3xx responses per rolling ~60s window**; exceeding it returns HTTP `420` and further requests are discarded until the window resets. Repeated violations can lead to application banning.

**Token-bucket group limits** (newer mechanism, applies per route-group):
- Headers `X-Ratelimit-Group`, `X-Ratelimit-Limit`, `X-Ratelimit-Remaining`, `X-Ratelimit-Used`, and `Retry-After` on 429s.
- Token cost per response: 2xx = 2 tokens, 3xx = 1 token, 4xx = 5 tokens, 5xx = 0 tokens (free — server errors don't count against you).
- Tokens refill on a floating window per group; buckets are keyed per group + per user/app.
- The only group relevant to this project's endpoints is **`market-order`: 12000 tokens / 15 minutes** (covers `GET /markets/{region_id}/orders/`), confirmed live (`x-ratelimit-group: market-order`, `x-ratelimit-limit: 12000/15m`). The character-orders and market-history endpoints did not carry an `x-rate-limit` block in the spec, so only the general error-limit applies to them.

**General guidance**: "Don't operate at the limit" — spread requests over time rather than bursting, and respect `Expires` rather than polling on a fixed short interval. There is no published flat requests/second cap; the effective throttle is the combination of per-route cache TTLs, the error-limit window, and (for market orders) the `market-order` token bucket.

## 7. Implications for polling design

- Character open-orders: poll no faster than every 20 min (cache TTL); realistically once every 20–30 min is plenty since order state changes are driven by the player's own actions.
- Character order history: poll no faster than every 60 min; 90-day retention means a full backfill needs at most one paginated sweep, then just tail the most recent page.
- Market history (per hub, per type_id): only refreshes once/day at 11:05 UTC — one fetch per type per day after that time is sufficient; there's no benefit to polling more often.
- Market order book (per hub): the *region* fetch is the expensive one — ~411 pages for The Forge to get Jita's data (fewer for Heimatar/Rens, not yet measured but expect far fewer given Rens's smaller trade volume). At 5-minute cache TTL and a 12000-tokens/15-min budget (2 tokens per 200-response page ≈ 822 tokens for a full Jita sweep), a full-region-then-filter-by-`location_id` sweep is well within budget even every 5 minutes, but doing it less often (e.g. every 15–30 min) is friendlier and still catches most opportunities given order books don't change that violently minute to minute.
