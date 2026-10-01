# ESI assets and character-orders endpoints for a trading-stock ledger

Question: for a station-trading **ledger** (what stock does the pilot hold at
the trading station, and which of their orders have filled/partially filled),
what do `GET /characters/{character_id}/assets/`
(`esi-assets.read_assets.v1`) and
`GET /characters/{character_id}/orders/` +
`GET /characters/{character_id}/orders/history/`
(`esi-markets.read_character_orders.v1`) actually return, how do you filter
assets to one station, does a character have multiple hangar divisions the way
a corporation does, and — the sharp edge — how is a fill (full or partial)
actually observable?

Written: **2026-10-01**. Schema and rate-limit claims are read directly from a
live fetch of `https://esi.evetech.net/meta/openapi.json` made on 2026-10-01
(`openapi 3.1.0`, same artefact family as
`docs/research/esi-sso-cli.md` and
`docs/research/eve-market-mechanics-and-esi.md`, both fetched one day earlier).
Live HTTP header probes were made the same day against
`https://esi.evetech.net`. Tags follow the sibling docs: **[stated]** (verbatim
in a primary source, cited) or **[inferred]** (reasoning connecting stated
facts). **This note does not repeat findings already established in the two
files above** — it cites them by section where relevant and focuses only on
the assets endpoint and fill-detection mechanics, which neither prior note
covers.

Bottom line for the ledger:

- **Assets give you a static snapshot, not a feed.** `GET
  /characters/{character_id}/assets/` returns `item_id, type_id, quantity,
  location_id, location_flag, location_type, is_singleton` (+
  `is_blueprint_copy`), paginated, cached 3600 s, rate-limited at 1800 tokens /
  15 min in its own `char-asset` bucket. There is no server-side station
  filter — you fetch every page and filter `location_id`/`location_type`
  client-side, and must walk the asset tree (`location_id` → parent `item_id`)
  to attribute items packed inside containers/ships back to a station.
- **A character has exactly one hangar per station.** The character assets
  schema's `location_flag` enum has a single `Hangar` value; `CorpSAG1`
  through `CorpSAG7` (the seven corp hangar divisions) exist **only** in the
  corporation assets schema. There is no character-side concept of multiple
  hangar divisions at one station — confirmed by diffing the two live
  OpenAPI enums directly (89 character values vs. 125 corporation values; the
  36-value difference is exactly the corp-only set: `CorpSAG1–7`,
  `OfficeFolder`, `Wallet`, `Impounded`, etc.).
- **Order history cannot tell you a fill happened.** `GET
  /characters/{character_id}/orders/history/`'s `state` field has exactly two
  enum values, **`cancelled` and `expired`** — no `fulfilled`/`closed`/`filled`
  value exists anywhere in the live schema. The route's own OpenAPI
  description says it returns "cancelled and expired market orders" — fully
  filled orders are, by CCP's own route scoping, out of its domain. This is a
  known, CCP-acknowledged gap (`esi/esi-issues#612`, CCP's own tracker), not an
  omission in a client library.
- **A full fill is only observable by disappearance + `volume_remain`, never
  by a status field.** While an order is still open, `volume_remain` reaching
  `0` on `/orders/` is the one authoritative, field-level signal of a complete
  fill. A **partial** fill is directly observable the same way —
  `volume_remain` decreasing between two polls of `/orders/` while `order_id`
  is unchanged. Once an order disappears from `/orders/` entirely, ESI gives
  you no further authoritative signal: it may or may not show up afterwards in
  `/orders/history/` with `state: cancelled` or `expired`; if it never shows up
  there, the only account for the gap is inference (it was filled), which no
  CCP source formally confirms.
- **Caching:** assets 3600 s / `char-asset` 1800 tokens-15 min; open orders
  1200 s / no rate-limit group; order history 3600 s / no rate-limit group —
  the latter two numbers are already recorded in
  `docs/research/eve-market-mechanics-and-esi.md` §6.5 and reconfirmed live
  here unchanged.

---

## Method / sources

Primary sources fetched 2026-10-01 unless noted:

| Source | URL / locator | Role |
|---|---|---|
| **ESI OpenAPI spec (live, fetched and parsed directly)** | `https://esi.evetech.net/meta/openapi.json` | `CharactersCharacterIdAssetsGet`, `CorporationsCorporationIdAssetsGet`, `CharactersCharacterIdOrdersGet`, `CharactersCharacterIdOrdersHistoryGet` schemas; per-route `x-cache-age`, `x-rate-limit`, `x-required-roles`, `page`/`X-Pages` parameters |
| Live header probe | `GET https://esi.evetech.net/latest/characters/1/assets/?page=1` | Confirmed live `x-ratelimit-group: char-asset`, `x-ratelimit-limit: 1800/15m`, `x-ratelimit-used: 5` on an (unauthenticated, 401) request — corroborates the 4xx-costs-5-tokens rule already recorded in `docs/research/esi-sso-cli.md` §6 item 12 |
| ESI docs — Best Practices | <https://developers.eveonline.com/docs/services/esi/best-practices/> | `expires`/`ETag`/`If-None-Match`/`last-modified` semantics (shared machinery, applies to assets and orders identically) |
| ESI blog — ESI ETag Best Practices (2018-05-02) | <https://developers.eveonline.com/blog/esi-etag-best-practices> | ETag is content-hash based, independent of `expires`; always store and resend it |
| ESI docs — X-Pages Pagination | <https://developers.eveonline.com/docs/services/esi/pagination/x-pages/> | `page` param / `X-Pages` header semantics; the pagination class both `/assets/` and `/orders/history/` use |
| ESI docs — Cursor-Based Pagination | <https://developers.eveonline.com/docs/services/esi/pagination/cursor-based/> | Confirms the newer `before`/`after` scheme exists but is **not** what assets/orders use |
| ESI blog — Changing pagination: turning a new page (2025-07-25) | <https://developers.eveonline.com/blog/changing-pagination-turning-a-new-page> | States cursor-based pagination is for **new** routes only; "existing routes … will remain offset-based for now" — i.e. assets/orders-history keep `page`/`X-Pages` |
| esi-docs (legacy, CCP-operated) — Asset `location_id` quick reference | <https://docs.esi.evetech.net/docs/asset_location_id.html> | `location_id` value-range table (station/system/abyssal/item ranges), asset-tree example, list of historically fixed asset bugs |
| **`esi/esi-issues` (CCP's own ESI tracker)**, issue #612 "Corp/Char Market Orders: Parity" | <https://github.com/ccpgames/esi-issues/issues/612> | CCP's own announcement of the orders/history route, scoped explicitly to cancelled/expired; reporter's explicit statement that filled/instant orders have no ESI equivalent |
| `esi/esi-issues` (via `ccpgames/esi-issues`), issue #610 "Bug in Assets: is_singleton is always false" | <https://github.com/ccpgames/esi-issues/issues/610> | `is_singleton` semantics (packed vs. unpacked, not simply `quantity>1`); CCP (`@ccp-zoetrope`) pagination-size policy quote |
| `esi/esi-issues`, issue #698 "Bug: Assets included items that was destroyed in-game" | <https://github.com/esi/esi-issues/issues/698> | Historical (fixed) asset staleness bug, cross-referenced in the legacy asset_location_id doc's "Fixed Bugs" list |
| `esi/esi-issues`, issue #1352 "500 Error in corporate assets … 'location_flag' is required" | <https://github.com/esi/esi-issues/issues/1352> | Corporation Projects hangar (`CorporationGoalDeliveries`) is a recent (2023) addition to the location_flag surface — evidence the enum is still actively extended, corp-side only |
| EVE Online Forums, Third Party Developers — "Calculating when an order was fulfilled" (2023-12-06) | <https://forums.eveonline.com/t/calculating-when-an-order-was-fulfilled/431246> | Community-confirmed (not CCP-stated) `volume_remain == 0` ⇒ fulfilled heuristic, and the dead end that order history doesn't help |
| EVE Online Forums — "ESI order history not showing all finished/canceled orders" (2022-07-06) | <https://forums.eveonline.com/t/esi-order-history-not-showing-all-finished-canceled-orders/369288> | Documents the 90-day-retention + repeatedly-modified-order gap where an order vanishes from both endpoints with no terminal state ever recorded |
| EVE Online Forums — "ESI - Endpoint for Corporate Hangar Divisions for non-Directors please?" (2023-11-12) | <https://forums.eveonline.com/t/esi-endpoint-for-corporate-hangar-divisions-for-non-directors-please/427425> | Corroborates `CorpSAG1`–`CorpSAG7` as the corp hangar-division location_flags, the recursive-tree cost of querying one division, and that no character-side equivalent exists |
| EVE Online Forums — "Mysterious LocationID" (2017-12-08) | <https://forums.eveonline.com/t/mysterious-locationid/44239> | Real example of an asset nested inside another asset (`location_id` pointing at a parent `item_id`, not a station) |

Everything with an `esi-issues`/`ccpgames` GitHub URL is CCP's own issue
tracker — CCP engineers reply directly in those threads and are quoted where
cited. The EVE Online Forums are CCP-operated (official "Third Party
Developers" category) but the *answers* quoted there are community-authored,
not CCP staff; each such quote is labelled **[community, uncontradicted by any
CCP source found]** rather than **[stated]**.

---

## 1. `GET /characters/{character_id}/assets/`

### 1.1 Response shape **[stated]**

From the live OpenAPI spec, schema `CharactersCharacterIdAssetsGet` (array of):

```json
{
  "item_id": 1000000016835,
  "type_id": 3516,
  "quantity": 1,
  "location_id": 60002959,
  "location_flag": "Hangar",
  "location_type": "station",
  "is_singleton": true,
  "is_blueprint_copy": true
}
```

All of `type_id, quantity, location_id, location_type, item_id, location_flag,
is_singleton` are `required`; `is_blueprint_copy` is optional and present only
on blueprint items. There is **no price, acquisition cost, or timestamp field**
anywhere on an asset row — assets tell you *what you hold and where*, never
*what you paid* or *when you got it*. A ledger needs the wallet
journal/transactions (already scoped in
`docs/research/eve-market-mechanics-and-esi.md` §6.5) to join cost basis onto
a stock position.

Field meanings, **[stated]**:

- `is_singleton`: *not* simply "`quantity == 1`". Per CCP's own bug-tracker
  thread, "Singleton: True is unpacked (not stackable), False is packed
  (stackable)" and "Quantity below 0 is used for things like blueprints"
  (`ccpgames/esi-issues#610`, closed, fix noted as shipped "in
  `/v3/characters/{character_id}/assets/`"). For a trading ledger: a packaged,
  stackable commodity sitting in a hangar has `is_singleton: false`, and
  `quantity` is the unit count you actually want. A fitted module or an
  unpacked ship has `is_singleton: true` and `quantity: 1`.
- `location_type` enum is exactly `station | solar_system | item | other`
  (live schema). `item` means the asset's `location_id` is **another asset's
  `item_id`** (it's inside a container, ship cargo hold, or fitted to a ship) —
  see [§1.3](#13-filtering-to-a-single-station).

### 1.2 Pagination **[stated]**

- `page` is an optional integer query parameter (`minimum: 1`); the response
  carries an `X-Pages` header, same offset-based mechanism as
  `/markets/{region_id}/orders/` (already documented in the sibling note §6.1)
  and described generically in ESI docs' *X-Pages Pagination* page.
- The live GET schema does **not** declare a `maxItems` on the response array
  (unlike the sibling `locations`/`names` POST bodies, which cap at
  `maxItems: 1000` request item-ids). Older/archived mirrors of the same route
  (e.g. the api-evangelist OpenAPI mirror, itself downstream of an earlier ESI
  spec generation — secondary, cited only for corroboration) show
  `maxItems: 1000` on the array; a CCP engineer states the underlying policy
  directly: "The default and most performant pagination size for ESI has been
  determined to be 1000. If an endpoint is still passing around large sets of
  data then we usually bring them down to this default size" —
  `@ccp-zoetrope`, `ccpgames/esi-issues#610`. **[inferred]** Expect ~1000
  assets per page; do not hard-code it as a contract, since the current live
  schema does not assert it.
- Per ESI's *Changing pagination: turning a new page* blog (2025-07-25),
  cursor-based (`before`/`after`) pagination is reserved for **new** routes;
  "existing routes … will remain offset-based for now." `/assets/` is not in
  the cursor-based cohort — confirmed by its `page`/`X-Pages` parameters in the
  live spec, not by the blog naming it directly. **[stated + inferred]**
- **No server-side filter parameter exists at all** — no `location_id`,
  `type_id`, or `location_flag` query parameter on this route in the live
  spec. Filtering to one station is entirely a client-side operation after
  paging through the full asset list. **[stated — absence verified against the
  live `parameters` array]**

### 1.3 Filtering to a single station

**[stated]** The legacy (CCP-operated) *Asset `location_id` quick reference*
page gives the exact `location_id` semantics an engine needs to filter and
tree-walk:

> - `item_id` of the parent asset (used to build the assets tree)
> - Item ID of the active ship in space … You can get the active ship by
>   combining data from [the location endpoint and the ship endpoint]
> - Asset Safety (`location_id == 2004`)
> - System ID (range: 30000000 – 32000000)
> - Abyssal System ID (range: 32000000 – 33000000)
> - Station ID (range: 60000000 – 64000000)
> - Structure ID — resolvable via the structure endpoint (if you have docking
>   rights) or the bookmark endpoint
> - Customs Office ID (not resolvable)
> - Corporation Office ID (not resolvable; link between hangars/divisions and
>   structure/station IDs when `location_flag` is `OfficeFolder`)
>
> — `docs.esi.evetech.net/docs/asset_location_id.html`, "Asset location_id
> quick reference" + worked flat-list/tree example

Concretely, for a station-trading ledger pinned to one NPC station:

1. Fetch every page of `/characters/{character_id}/assets/`.
2. Keep rows where `location_type == "station"` **and** `location_id ==
   <your station id>` (station IDs fall in `60000000–64000000`, per the quote
   above) — these are the items sitting directly in a hangar, cargo hold, drop
   box, etc. at that station, one level deep.
3. For any asset whose `location_type == "item"`, its `location_id` is another
   asset's `item_id` (a container, a ship's cargo, a fitted module) — **not** a
   station. To attribute it to a station you must walk up the parent chain
   (`location_id` → that `item_id`'s own row) until you hit a row whose
   `location_type` is `station`/`solar_system`/`other`. This is exactly the
   "Flat List" → "Tree" transformation CCP's own doc walks through. **[stated]**
   A live forum example of this in the wild: an asset whose `location_id` was
   itself another asset's `item_id` (not resolvable as a structure), confirmed
   by a community reply as "Probably a container that that item is in. Check
   to see if another object has an item_id of `<that number>`" —
   *Mysterious LocationID*, forums.eveonline.com, 2017-12-08.
   **[community, uncontradicted]**
4. `location_flag` narrows *within* a location — e.g. `Hangar` (general
   personal hangar), `AssetSafety` (forced relocation after a structure
   death/eviction; `location_id` becomes `2004` per the quote above), `Cargo`
   (in a ship's cargo hold while docked), `Deliveries`/`CapsuleerDeliveries`
   (contract/market deliveries not yet moved into the hangar). **[stated — enum
   values from the live schema; semantics from the quoted doc + enum naming]**
   For a station-trading stock ledger, `Hangar` is almost certainly the only
   flag of interest; `Cargo`/`Deliveries`/`AssetSafety` rows at the target
   station represent stock the pilot has not finished moving into sellable
   position and **[inferred]** should probably be surfaced as a warning rather
   than counted as tradeable inventory.

### 1.4 Hangar divisions: one for a character, up to seven for a corporation

**[stated — read directly off the two live schemas]** Diffing
`CharactersCharacterIdAssetsGet.items.properties.location_flag.enum` (89
values) against `CorporationsCorporationIdAssetsGet.items.properties
.location_flag.enum` (125 values) from the same `openapi.json` fetch:

- The **character** enum contains exactly **one** hangar-type value: `Hangar`.
  It also lists `HangarAll`, but — see the caveat below — no example payload
  in any source checked (the live schema, the legacy Swagger archive, or any
  of several independently-generated community client libraries' enum dumps)
  ever shows an actual asset row with `location_flag: "HangarAll"`. Treat it as
  reserved/filter-only, not a value a real asset carries. **[inferred — an
  absence across every corroborating source, not a positive CCP statement]**
- The **corporation** enum additionally contains `CorpSAG1` through `CorpSAG7`
  — the seven corporation hangar divisions — plus `OfficeFolder`, `Wallet`,
  `Impounded`, `CorpDeliveries`, and 30 other corp-only values. The 36-value
  set present in the corporation enum and absent from the character enum is
  **exactly** `{Bonus, Booster, Capsule, CorpDeliveries, CorpSAG1..7,
  CrateLoot, DustBattle, DustDatabank, Impounded, JunkyardReprocessed,
  JunkyardTrashed, OfficeFolder, Pilot, PlanetSurface, QuantumCoreRoom,
  Reward, SecondaryStorage, ServiceSlot0..7, ShipOffline, SkillInTraining,
  StructureActive, StructureFuel, StructureInactive, StructureOffline,
  Wallet}` — confirmed by set-diffing the two live enums directly, 2026-10-01.
- **Answer to the brief's question:** a character does **not** have multiple
  hangar divisions at one station the way a corporation does. A character's
  entire personal hangar at a station is one bucket, `location_flag: "Hangar"`.
  Corporation hangar divisions (`CorpSAG1`–`7`) are a corp-only concept, and —
  per the live `GET /corporations/{corporation_id}/assets/` route's
  `x-required-roles: ["Director"]` — reading them at all requires the
  `Director` corporate role (confirmed live from the OpenAPI route object,
  same mechanism as the corp-role gating already documented generically in
  `docs/research/esi-sso-cli.md` §5). **[stated]**
- This is corroborated by a community forum thread specifically about corp
  hangar-division visibility: "For example, the 1st corp hangar [is]
  represented as a flag CorpSAG1 on an asset. BUT that's only for the top
  level: a corp hangar that contains containers will require more complex
  queries" and "only a corp director could use that ESI endpoint" —
  *ESI - Endpoint for Corporate Hangar Divisions for non-Directors please?*,
  forums.eveonline.com, 2023-11-12. **[community, uncontradicted]** No parallel
  character-side request or endpoint exists in the live spec, because there is
  nothing to divide — a character has one hangar per station.
- `CorporationGoalDeliveries` is a recent (2023, Corporation Projects feature)
  addition to the location_flag surface, and its rollout briefly broke
  `GET /corporations/{corporation_id}/assets/` with a 500 for assets in the
  new project hangar before the flag was added to the spec
  (`esi/esi-issues#1352`, closed/fixed). **[stated]** This is corp-only and
  irrelevant to a character ledger, but shows the location_flag enum is still
  actively extended on the corporation side only.

### 1.5 Cache / ETag behaviour **[stated]**

Live OpenAPI route extensions on `GET /characters/{character_id}/assets/`:

```json
"x-cache-age": 3600,
"x-client-cache-ttl": 3600,
"x-server-cache-mode": "ttl-based",
"x-server-cache-ttl": 3600
```

The route declares the standard `ETag`, `Last-Modified` response headers and
accepts `If-None-Match` / `If-Modified-Since` request parameters (all via
shared `components.parameters`/`components.headers` refs, identical machinery
to every other ESI route). General ETag discipline — "store the ETag
independently of the cache expiry … Content that has expired can be retried
with the last known ETag. If the content hasn't changed, [you get] a `304 Not
Modified`" — per ESI's dedicated *ESI ETag Best Practices* blog (2018-05-02),
which predates and generalizes the per-route cache-TTL numbers found in the
spec. **[stated]** The *X-Pages Pagination* docs add the one assets-specific
caveat: "If the cache expires between fetching two different pages, you may
see duplicated items" — mitigate by not starting a multi-page assets fetch
within a few seconds of the known `expires` time. **[stated]**

### 1.6 Rate limits **[stated — live-verified]**

```json
"x-rate-limit": {"group": "char-asset", "max-tokens": 1800, "window-size": "15m"}
```

— on all three character asset routes (`GET /assets/`, `POST
/assets/locations/`, `POST /assets/names/`), live `openapi.json`. Live header
probe against `GET /latest/characters/1/assets/?page=1` (deliberately
unauthenticated, got a 401) returned:

```
x-ratelimit-group: char-asset
x-ratelimit-limit: 1800/15m
x-ratelimit-remaining: 1795
x-ratelimit-used: 5
```

confirming both the group/limit **and** the general token-cost rule already
recorded in `docs/research/esi-sso-cli.md` §6 item 12 (4xx responses cost 5
tokens) — a 401 here consumed 5 of the 1800-token `char-asset` budget. For a
single-station ledger that re-polls assets occasionally, 1800 tokens/15 min at
2 tokens per successful page (per the generic 2XX-costs-2 rule) comfortably
covers dozens of asset-list refreshes even at several pages each; budget
pressure would only appear from polling many characters' full inventories in
parallel. **[inferred arithmetic from stated per-call costs]**

### 1.7 Known (historical, now-fixed) data-quality bugs **[stated]**

Worth knowing before trusting a snapshot literally, both fixed and listed by
CCP itself:

- **Destroyed items lingered in the asset list.** "Character Assets include
  items that no longer exist in game… returns items fitted to ships that was
  destroyed" — `esi/esi-issues#698`, opened 2018-01-02. Listed as a *Fixed Bug*
  ("Returned destroyed assets") in the legacy `asset_location_id.html` doc's
  "Fixed Bugs (only relevant for historic data)" section — i.e. CCP confirms
  this no longer happens on the live route, but it establishes that a stale
  asset row has occurred in ESI's history and that trusting `item_id`
  existence absolutely requires using the current data, not a cached copy from
  before a known-bad window.
- **`is_singleton` was once always `false`.** `ccpgames/esi-issues#610`
  (closed) — "This fix is released and present in
  `/v3/characters/{character_id}/assets/` and
  `/v2/corporations/{corporation_id}/assets/`" — both well below the versions
  the current unversioned (`X-Compatibility-Date`-gated) routes serve, so the
  live route is unaffected. **[stated]**
- The same legacy doc's "Fixed Bugs" list also records historical returns of
  "9e18 locations" (garbage `location_id` values, matching the *Mysterious
  LocationID* forum report above), trained skills/active boosters
  erroneously appearing in the asset list, PI structures appearing, and
  character IDs appearing as `location_id` — all marked fixed, "only relevant
  for historic data." **[stated]**

---

## 2. `GET /characters/{character_id}/orders/` and `/orders/history/`

### 2.1 Response shape **[stated]**

Live OpenAPI schemas, both `array` of (fields identical between the two
routes except `state`, which only `orders/history` has):

```json
{
  "order_id": 123,
  "type_id": 456,
  "region_id": 10000030,
  "location_id": 60004588,
  "range": "station",
  "is_buy_order": true,
  "is_corporation": false,
  "price": 33.3,
  "volume_total": 123456,
  "volume_remain": 4422,
  "min_volume": 1,
  "duration": 30,
  "issued": "2016-09-03T05:12:25Z",
  "escrow": 45.6,
  "state": "expired"
}
```

`required` on `/orders/`: `is_corporation, duration, order_id, type_id,
region_id, location_id, range, price, volume_total, volume_remain, issued`.
`/orders/history/` requires the same set plus `state`. `escrow` and
`min_volume` are optional (buy-order-only in practice, per their
descriptions). **[stated]**

### 2.2 Fields for matching an order back to a ledger entry

**[stated + inferred]** For joining an ESI order row to a ledger entry the
engine already created when it placed/recommended the order:

- **`order_id`** is the one stable, unique join key — "Unique order ID"
  (OpenAPI description) — present and unchanged across every poll of
  `/orders/` and, if the order surfaces there, in `/orders/history/` too.
- **`type_id` + `location_id` + `issued` + `is_buy_order`** together
  reconstruct "which ledger row is this" if the ledger was seeded before the
  order existed in ESI (e.g. placed seconds ago and not yet visible) — `issued`
  is at second resolution, so a ledger keyed only by `(type_id, is_buy_order,
  price)` risks a collision if the same price is relisted; prefer `order_id`
  once it's known and fall back to `(type_id, location_id, is_buy_order,
  price, issued)` only for the brief order-placed-but-not-yet-polled window.
- **`volume_total`** is fixed at creation; **`volume_remain`** is the live
  quantity still open — the pair gives filled-so-far = `volume_total −
  volume_remain` at any poll while the order is still active.
- **`price`** is "cost per unit for this order" (OpenAPI) — multiply by
  filled-so-far for a running fill value, independent of the wallet journal.
- **`state`** exists **only** on `/orders/history/` and is one of exactly
  `cancelled` or `expired` — see [§2.3](#23-how-a-fill-is-actually-observable)
  for why this does not help detect a fill.

### 2.3 How a fill is actually observable

This is the sharp edge the brief asked to focus on, and it is **not
resolvable from a single documented field**. Pulling together every primary
source fetched:

**2.3.1 The `state` enum literally cannot represent a fill. [stated]**

```json
"state": {"description": "Current order state", "type": "string",
          "enum": ["cancelled", "expired"]}
```

— live `CharactersCharacterIdOrdersHistoryGet` schema, `openapi.json`,
fetched 2026-10-01. There is no third value. The route's own summary text
agrees: *"List cancelled and expired market orders placed by a character up to
90 days in the past."* — same spec, `description` of
`GetCharactersCharacterIdOrdersHistory`. The scoping is deliberate, not a
documentation gap: CCP's own tracker records the route's original
announcement in those exact terms:

> "Just released [...] `get_characters_character_id_orders_history` and
> [...] `get_corporations_corporation_id_orders_history` allowing you to see a
> corporation's or character's **cancelled or expired** orders going 90 days
> back."
> — CCP reply, `esi/esi-issues#612` ("Corp/Char Market Orders: Parity"),
> closed

And the issue that prompted that route's creation says outright, before the
route existed, that ESI has no equivalent for filled orders at all:

> "The ESI endpoints only include active orders [...] Orders that was
> completed immediately [...] there is no way to get those types of orders
> with ESI [...] If you keep history of Market Orders, you need to know why
> and how each order was closed. This is no longer possible with ESI, but,
> was possible with the XmlApi."
> — `GoldenGnu`, `esi/esi-issues#612`, opened as a parity request against the
> legacy XML API

The `orders/history` route that CCP shipped in response **only closed the
cancelled/expired gap**, not the filled-order gap the issue also asked about.
**[stated — this is the issue's own framing, and the shipped route's scope
matches it exactly]**

**2.3.2 Full fill: only observable as disappearance with a documented
`volume_remain` precondition. [stated field semantics + community inference]**

- While the order is **still listed** in `/orders/`, `volume_remain` reaching
  `0` is a direct, field-level signal that the order is fully matched — this
  follows from the stated field description ("Quantity of items still
  required or offered") without needing any inference. In practice an order
  is very unlikely to be observed at exactly `volume_remain: 0` before it
  disappears (fills and removal happen together), so this is a narrow window.
- The operative signal in practice is **disappearance from `/orders/`
  entirely**. A community answer in CCP's own developer forum states this
  plainly:

  > "You can use `volume_remain`: `volume_remain` is the items left in the
  > order, when it's 0 (zero) the order is fulfilled."
  > — `Golden_Gnu`, *Calculating when an order was fulfilled*,
  > forums.eveonline.com, 2023-12-08 **[community, uncontradicted]**

  The original poster then correctly points out this doesn't compose with
  `/orders/history/`'s scope ("the order should already have expired, and so
  `volume_remain` should always === 0" if it only ever contains expired/
  cancelled orders) and concludes: "Looks like I have no alternative but to
  poll the API to find out when an order was fulfilled [...] and maintain
  local state." — same thread. **[community, uncontradicted by any CCP
  source found]**
- **No CCP-authored source states that a fully filled order is guaranteed to
  never appear in `/orders/history/`, nor that it is guaranteed to disappear
  cleanly.** The closest CCP statement is the route-scoping text quoted in
  §2.3.1. Treat "filled orders never reach order history" as the engine's
  working assumption (consistent with every source found), not as a stated
  guarantee: **[inferred, not stated]**.

**2.3.3 Partial fill: directly observable, with no ambiguity. [stated]**

Unlike a full fill, a **partial** fill requires no inference at all: poll
`/orders/`, and for any `order_id` that is still present, a decrease in
`volume_remain` since the previous poll (with `volume_total` and `order_id`
unchanged) is by definition a partial fill of `volume_total − volume_remain`
cumulative units, price `price` per unit. This is a direct reading of the
stated field semantics, not an inference from absence — the order is still
there, and the field changed in exactly the way its description says it
tracks. **[stated]**

**2.3.4 The practical polling algorithm this leaves you with:**

1. Poll `/orders/` on a cadence inside its 1200 s cache window
   ([§2.4](#24-cache--etag-behaviour)). For every `order_id` already in the
   ledger, diff `volume_remain` — any decrease is an authoritative partial
   fill, record it immediately.
2. When an `order_id` that was previously present is **absent** from a poll,
   query `/orders/history/` for that `order_id` (filtering client-side; there
   is no `order_id` query parameter — see [§2.5](#25-rate-limits)). If found
   with `state: cancelled` or `state: expired`, that is the authoritative
   terminal state and the last-seen `volume_remain` is the final unfilled
   remainder.
3. If the `order_id` is **not** found in `/orders/history/` (checked within
   the stated 90-day window — OpenAPI description, "up to 90 days in the
   past"), the engine's only remaining signal is the last-seen
   `volume_remain` from step 1: `0` → treat as a confirmed full fill (per
   §2.3.2's field semantics); `>0` → the order vanished without a recorded
   reason ESI will ever surface. This exact failure mode — an order
   disappearing with no terminal record, especially one repriced enough times
   that its `issued` stays within 90 days even though its true placement
   doesn't — is independently documented by a corporation-API user hitting the
   identical problem:

   > "it may happen that orders have to be changed several times and it will
   > remain on market over several months (more than 90 days). In that case
   > the history-order-list will never list that order [...] it will just
   > drop off the open-order-list and never can receive a finalized status for
   > it: has it been canceled [...] or has the order bee[n] fullfilled?"
   > — *ESI order history not showing all finished/canceled orders*,
   > forums.eveonline.com, 2022-07-06 **[community, uncontradicted]**

   **[inferred]** For the ledger, step 3's `volume_remain > 0` case should be
   surfaced to the user as "unknown outcome," not silently resolved either
   way — no primary source supports guessing cancelled vs. filled in that
   case.

### 2.4 Cache / ETag behaviour **[stated — reconfirmed live, unchanged from the sibling note]**

```json
// GET /characters/{character_id}/orders/
"x-cache-age": 1200, "x-client-cache-ttl": 1200, "x-server-cache-ttl": 1200

// GET /characters/{character_id}/orders/history/
"x-cache-age": 3600, "x-client-cache-ttl": 3600, "x-server-cache-ttl": 3600
```

— both `x-server-cache-mode: ttl-based`, both carrying the standard
`ETag`/`Last-Modified`/`If-None-Match`/`If-Modified-Since` machinery. These
exact numbers are already recorded in
`docs/research/eve-market-mechanics-and-esi.md` §6.5's adjacent-endpoints
table; this fetch (2026-10-01, one day later) confirms they are unchanged.
**[stated]** `/orders/history/` additionally returns `X-Pages` and accepts a
`page` parameter, which `/orders/` does not — `/orders/` returns the
character's entire open-order list (bounded by the 305-order cap derived in
the sibling note §4.1) in one uncapped response. **[stated — presence/absence
of the `page` parameter verified directly against the live `parameters`
arrays of both routes]**

### 2.5 Rate limits **[stated — reconfirmed live]**

Neither route declares an `x-rate-limit` group in the live spec — both
`/orders/` and `/orders/history/` have no `x-rate-limit` key at all,
re-verified directly against the 2026-10-01 `openapi.json` fetch (matching
the sibling note's §6.5 "none" entries exactly). Both routes are therefore
governed only by the global error limiter (100 non-2xx/3xx responses per 60 s
→ HTTP 420), already documented in `docs/research/esi-sso-cli.md` §6 item 12
and `docs/research/eve-market-mechanics-and-esi.md` §6.6. **[stated]** There
is **no `order_id` (or any other) query filter** on either route — "match by
`order_id` client-side after fetching the full response" is the only option,
same absence-of-filter pattern as assets ([§1.2](#12-pagination)). **[stated —
verified against the live `parameters` arrays]**

---

## 3. Disagreements / silences between sources

1. **Assets page-size cap: documented in archived mirrors, silent in the live
   spec.** The live `CharactersCharacterIdAssetsGet` schema carries no
   `maxItems`; several secondary mirrors (api-evangelist's OpenAPI
   regeneration, community client-generated docs) show `maxItems: 1000`
   inherited from an earlier spec generation, and a CCP engineer states the
   policy reason directly (`ccpgames/esi-issues#610`, quoted in §1.2).
   **Winner: treat ~1000/page as the expected behaviour (CCP's own policy
   quote), but do not assert it as a hard contract** since the current live
   schema is silent on the number. **[inferred]**
2. **Whether a filled order is *guaranteed* never to reach `/orders/history/`
   vs. simply *usually* not.** CCP's own issue tracker (`esi/esi-issues#612`)
   states the route's scope is "cancelled or expired" and that, before the
   route existed, "there is no way to get [filled] types of orders with ESI."
   No CCP source positively states "a filled order will never appear in
   history," only that the route's *scope* excludes them by design.
   **Winner: treat the community forum consensus (§2.3.2–2.3.4) as the correct
   practical model, but keep the distinction between CCP's stated route-scope
   and the community's inferred disappearance behaviour** — they are
   consistent, not identical claims.
3. **`HangarAll` enum presence vs. observed usage.** Both the character and
   corporation `location_flag` enums list `HangarAll` as a valid value, but no
   example payload in any source checked (live spec examples, legacy Swagger,
   or any independently-generated community client's example JSON) shows an
   actual asset row carrying it. **Winner: treat it as present-in-enum,
   absent-in-the-wild** — an inference from a consistent absence across every
   source, not a CCP statement either way.

---

## 4. Open questions / not found

- **No CCP-authored sentence states what happens, end to end, to a fully
  filled order inside ESI.** The closest primary materials are the
  route-scoping text and the original parity-request issue (§2.3.1); the
  disappearance-based detection algorithm in §2.3.4 is the best available
  synthesis but is **not** CCP-confirmed as complete or correct for every
  case — particularly the "vanished after >90 days of being open, never in
  history" failure mode reported in the 2022 forum thread, which no CCP
  source has addressed.
- **No documented delta/change-notification mechanism for assets.** Per the
  cursor-based-pagination blog, that scheme is reserved for new routes;
  `/assets/` stays on `page`/`X-Pages`, which has no "what changed since my
  last poll" primitive. A ledger must diff full snapshots itself.
- **Whether items in an Upwell structure's corp hangar divisions, or a
  character's assets stored in a structure the character does not have
  docking rights to, resolve `location_id` to anything usable** is not
  re-verified here; the sibling note (`eve-market-mechanics-and-esi.md` §6.3)
  already flags the general structure-market docking-access gap as unverified
  in any first-party source, and nothing fetched for this note closes it for
  assets specifically.
- **No documented maximum for how many asset pages a character can have** nor
  any stated cap distinct from the inferred ~1000-per-page default
  (§1.2/§3.1).
- **No live numeric test of the 90-day order-history retention boundary** was
  performed for this note (would require an order genuinely 90+ days old);
  the 90-day figure is taken verbatim from the OpenAPI route description only.
