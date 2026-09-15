# Research: data source for Rens market data (v1)

**Ticket:** [mgoodness/eve-trader#2](https://github.com/mgoodness/eve-trader/issues/2) (part of the v1 wayfinder map, #1)
**Date:** 2026-09-14
**Scope:** Rens station only, Heimatar region only, single-user low-traffic tool on a GCP e2-micro VM.

## Recommendation (TL;DR)

**Use ESI alone for v1.** No third-party source is required.

- **Order book:** `GET /markets/{region_id}/orders/` for `region_id=10000030` (Heimatar), paginated, filtered client-side to `location_id == 60004588` (Rens VI - Moon 8 - Brutor Tribe Treasury). ESI has no per-NPC-station order endpoint, so pulling the whole region and filtering locally is the standard, necessary pattern.
- **Liquidity/volume:** `GET /markets/{region_id}/history/` for the same region_id, per `type_id`, gives exactly the daily volume/order-count/price-range data needed for liquidity filtering. It's region-scoped, not station-scoped, but Rens is one of the five major trade hubs and very likely dominates Heimatar's trade volume, so region history is a reasonable liquidity proxy for a v1 heuristic.
- Respect ESI's 300s order-book cache and the once-daily (11:05 UTC) history cache via `Expires`/`ETag`, and poll no more often than that. Rate limits are nowhere close to being a constraint for this workload.
- A third-party aggregator (Fuzzwork) is a legitimate *optional* convenience/fallback — it directly supports Rens by station ID and would cut ~73 paginated requests down to one small call — but it's not necessary, it's a redundant pass-through of the same ESI-origin data, its aggregate endpoint doesn't return individual orders (so you'd still need ESI for the actual tradeable order rows), and it adds a dependency on a single-maintainer hobby service with no SLA. Revisit only if pagination/parsing overhead becomes a measured problem on the e2-micro.
- EVE Marketer should be avoided — it returned `503` at time of writing and has a long history of unreliability. adam4eve is up but has no documented, stable JSON API suited to backend automation; it's a browsing/analytics site.

## 1. ESI market endpoints

Primary sources: `https://esi.evetech.net/ui/` (Swagger UI), `https://developers.eveonline.com/`, `https://docs.esi.evetech.net/docs/esi_introduction.html`, and live requests against `https://esi.evetech.net/latest/...` made on 2026-09-14/15.

### 1.1 Identifiers confirmed live

Verified via ESI universe endpoints (`/universe/regions/{id}/`, `/universe/ids/`):

- Heimatar region: `region_id = 10000030`
- Rens solar system: `system_id = 30002510`
- Rens station ("Rens VI - Moon 8 - Brutor Tribe Treasury"): `station_id = 60004588`

### 1.2 Order book: `GET /markets/{region_id}/orders/`

- Returns open market orders for an entire **region**. Query params: `order_type` (`buy`/`sell`/`all`, default `all`), optional `type_id`, `page`.
- **There is no NPC-station-scoped order endpoint.** The only station/structure-scoped market endpoint is `GET /markets/structures/{structure_id}/`, and that's for player-owned Upwell structures, requires an auth token and the `esi-markets.structure_markets.v1` scope, and doesn't apply to NPC stations like Rens's. For an NPC station you must pull the whole region and filter client-side by `location_id`.
- Live test against Heimatar (`region_id=10000030`, no `type_id` filter) returned **`X-Pages: 73`**, ~238 KB for page 1 alone, so a full region pull is on the order of ~15–20 MB total. This is because Heimatar/Rens is genuinely one of the busier regions (Rens is a major trade hub), so the "pull everything, filter locally" pattern here isn't free, but it is the only option ESI offers for a public NPC station.
- Observed live response headers (2026-09-14):
  ```
  cache-control: public
  etag: "bbf2416fb1cda2a3eb616d04d009396b2aabc497bb6091072ba1d3cf"
  expires: Tue, 15 Sep 2026 02:15:03 GMT   # ~5 min after the request
  last-modified: Tue, 15 Sep 2026 02:10:03 GMT
  x-esi-cache-status: HIT
  x-pages: 73
  x-ratelimit-group: market-order
  x-ratelimit-limit: 12000/15m
  x-ratelimit-remaining: 11998
  ```
  This matches ESI's documented cache behavior: the order-book route is cached for ~300 seconds server-side; polling more often than that returns identical cached data (confirmed by `x-esi-cache-status: HIT`) and just burns your own request/rate budget for nothing.

### 1.3 History (liquidity/volume): `GET /markets/{region_id}/history/`

- Per ESI's own docs (via the API explorer / OpenAPI spec): "Return a list of historical market statistics for the specified type in a region. **This route expires daily at 11:05**."
- Requires `type_id` as a query param (one call per item type). Response is an array of daily rows:
  ```json
  { "date": "2023-10-26", "highest": 1600.0, "lowest": 1400.0, "average": 1500.5,
    "volume": 10000, "order_count": 50 }
  ```
  `volume` (units traded that day) and `order_count` are exactly the liquidity signals wanted for filtering/ranking opportunities.
- Live test (`region_id=10000030`, `type_id=34` — Tritanium) confirmed this behavior:
  ```
  expires: Tue, 15 Sep 2026 11:05:00 GMT
  last-modified: Mon, 14 Sep 2026 11:03:28 GMT
  etag: W/"3877be5b4acd05ddb30577007775fbddc053302a2032b57f0e6c154b"
  ```
  i.e., the whole route is cached once per day, resetting at a fixed 11:05 UTC. Polling it more than once a day is pointless; ETag/If-None-Match support means a naive poller that ignores this still just gets cheap 304s.
- **Caveat:** this is region-level, not station-level. ESI has no station-scoped trade history for NPC stations. For Rens this is a reasonable proxy given Rens's status as a major hub within Heimatar, but it's an approximation worth flagging in the code/comments, not a precise Rens-only number.
- This satisfies the ticket's open question directly: **yes, ESI alone provides history/volume data usable for liquidity filtering** — no third party is required for that piece.

### 1.4 Rate limits and caching, generally

Per ESI's official introduction docs (`docs.esi.evetech.net/docs/esi_introduction.html`):

- **Caching headers:** `expires` (when new data will be available) and `last-modified` (when the underlying data last changed). Clients are expected to honor `expires` and not "circumvent caching" — ESI's docs explicitly warn that doing so risks a ban.
- **Error-limit headers:** `X-ESI-Error-Limit-Remain` / `X-ESI-Error-Limit-Reset` — a rolling window (currently observed as 100 errors / ~60s in test responses) that only decrements on *erroring* requests (4xx/5xx), not successful ones.
- **Newer per-route throughput limit:** current live responses also carry `X-Ratelimit-Group`, `X-Ratelimit-Limit`, `X-Ratelimit-Remaining`, `X-Ratelimit-Used` headers (e.g., `market-order` group at `12000/15m`, seen live above). This is far more generous than anything a single-user, single-station poller run every few minutes would ever approach — a full 73-page Heimatar pull once every 5 minutes is ~876 requests/day, nowhere near the 12000-per-15-minutes ceiling.
- **Operational best practice (not optional):** ESI requires/strongly expects a descriptive `User-Agent` header identifying the application and contact info; this should be set on all requests eve-trader makes.

**Bottom line on rate limits:** for this workload (one region, polled at most every 5 minutes, one history call per tradable item per day), ESI's rate limits are not a real constraint. The practical cost is bandwidth/CPU to page through and parse ~15–20 MB of JSON per region poll on a small VM, not hitting any throttle.

## 2. Third-party alternatives

### 2.1 Fuzzwork Market Data (`https://market.fuzzwork.co.uk/`)

- Actively maintained and live as of 2026-09-14 (checked directly; site responds, API responds with current data, current cache headers).
- Offers a station/system/region-scoped **aggregates** endpoint, e.g.:
  `https://market.fuzzwork.co.uk/aggregates/?station=60004588&types=34,35`
  — and its own docs explicitly list **`Rens - 60004588`** as one of the built-in station shortcuts, confirming Rens is directly supported.
- Response is a pre-computed aggregate per type (buy/sell weighted average, max/min, stddev, median, volume, order count, percentile) — **not the individual order rows** (no `order_id`, `min_volume`, `range`, `duration`, `issued`). That means it cannot, by itself, replace the raw order book needed to actually identify and act on specific buy/sell order pairs for station trading; it's aggregate/statistical, sourced from ESI in the first place.
- Live response headers show `Cache-Control: public, max-age=300` — the same 5-minute cadence as ESI's own order cache, because it's built from ESI's own order snapshots.
- Also offers periodic full order-book **CSV dumps** (`orderset-NNNNN.csv.gz`, `latest.csv.gz`) covering "all systems," which could serve as a bulk-download alternative or fallback to paginating ESI directly, at the cost of depending on Fuzzwork's own update cadence and availability.
- No formal SLA; it's a single-maintainer community project (long-running, well-regarded in the EVE community, but still a single point of failure with no support guarantee).

### 2.2 EVE Marketer (`https://evemarketer.com/`, API at `api.evemarketer.com`)

- **Returned HTTP 503 at time of testing** (both the site and the `marketstat` API endpoint). This is consistent with EVE Marketer's known long-running reliability problems — it has been intermittently unavailable/unmaintained for years within the community. **Not recommended** as a dependency for v1.

### 2.3 adam4eve (`https://www.adam4eve.eu/`)

- Site is live and actively serving pages (margin finder, order depth, trade volume by region/type, orderbook age, etc. — useful analyst-facing tooling).
- However, it does not expose a clean, documented public JSON API suited to backend automation the way Fuzzwork's `/aggregates/` endpoint does; it's oriented around interactive browsing/reporting rather than being consumed programmatically by another service. Not a good fit as an ingestion source for this project.

## 3. Does ESI alone cover liquidity/volume filtering?

**Yes.** `GET /markets/{region_id}/history/` gives daily `volume` and `order_count` per `type_id`, which is exactly the shape of data needed to rank/filter opportunities by liquidity, with no third-party source required. The only caveat is that it's region-level (Heimatar), not Rens-station-level — acceptable for v1 given Rens is a dominant hub within that region, but worth a code comment/ADR note if this assumption ever needs revisiting (e.g., if Heimatar trade outside Rens turns out to be more significant than assumed).

## 4. Reasoning tied to v1 constraints

- **Single station, single region, single user, low traffic:** the workload is tiny relative to ESI's limits in every dimension (page count, request rate, daily history calls). There's no capacity reason to reach for a third party.
- **e2-micro resource constraints:** the real cost is parsing ~15–20 MB of paginated JSON per region poll, which is a CPU/bandwidth question, not a rate-limit question. This is a modest, one-time-per-poll cost (e.g., every 5 minutes) that a tiny VM can absorb; it doesn't scale with usage or seats since it's single-user.
- **Fewer moving parts / one source of truth:** ESI is the authoritative, official source; everything else (Fuzzwork included) is downstream of it. Adding a third-party dependency for v1 would mean depending on another service's uptime and update cadence to get *the same underlying data*, for no capability gain that matters at this scale (the extra capability Fuzzwork offers — one lightweight station-scoped call instead of paginating the region — is a bandwidth/convenience optimization, not something v1 strictly needs).
- **When a third party would start to matter:** if v1 later expands to multiple stations/regions (per the map's fog), the "pull the whole region and filter" pattern stops scaling as cleanly, and Fuzzwork's station-scoped aggregates (or its bulk CSV dumps) become a more attractive optimization. That's a good candidate to revisit explicitly if/when the map's scope grows beyond Rens-only.

## Sources

- ESI Swagger UI: <https://esi.evetech.net/ui/>
- ESI developer portal: <https://developers.eveonline.com/>
- ESI introduction docs (caching & error-limit headers): <https://docs.esi.evetech.net/docs/esi_introduction.html>
- ESI OpenAPI / API Explorer route docs (`/markets/{region_id}/orders`, `/markets/{region_id}/history`, `/markets/structures/{structure_id}`): <https://developers.eveonline.com/api-explorer>
- Live requests against `https://esi.evetech.net/latest/...` and `https://esi.evetech.net/latest/universe/...`, made 2026-09-14/15 (headers and payloads captured above)
- Fuzzwork Market Data, site and API docs: <https://market.fuzzwork.co.uk/>, <https://market.fuzzwork.co.uk/api/>
- EVE Marketer: <https://evemarketer.com/> (returned 503 at time of testing, 2026-09-14)
- adam4eve: <https://www.adam4eve.eu/>
