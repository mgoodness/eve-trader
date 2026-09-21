# Character standings in broker-fee calculations (Rens VI - Moon 8 - Brutor Tribe Treasury)

**Question.** How to include character NPC standings in EVE Online broker-fee
calculations for a tool that computes NPC-station trading fees at station
`60004588` (Rens VI - Moon 8 - Brutor Tribe Treasury, Heimatar `10000030`).

**Retrieved:** 2026-09-21. All live ESI/SDE calls made on that date.

## Sources used (primary first)

| # | Source | Version / identifier |
|---|--------|----------------------|
| S1 | ESI OpenAPI spec — `https://esi.evetech.net/meta/openapi.json` | info title `EVE SKINR Ingenuity (ESI) - tranquility`, spec `version: 2020-01-01`, fetched 2026-09-21 |
| S2 | Live ESI, `https://esi.evetech.net/latest/...` | observed 2026-09-21 |
| S3 | CCP Static Data Export (SDE), JSON Lines variant | `latest.jsonl` build **3528119**, releaseDate `2026-09-21T11:13:46Z`; zip `eve-online-static-data-3528119-jsonl.zip` |
| S4 | CCP Support — "Broker Fee and Sales Tax", article `203218962` (Zendesk API) | `edited_at 2026-03-02T13:51:50Z` |
| S5 | CCP Support — "Buy and Sell Orders", article `203218932` (Zendesk API) | `edited_at 2020-03-10T11:18:56Z` |
| S6 | EVE University Wiki — "Trading" (`action=raw`) | fetched 2026-09-21 |
| S7 | EVE University Wiki — "NPC standings" (`action=raw`) | fetched 2026-09-21 |
| S8 | `esi/esi-issues` GitHub issues #917, #457 (historical, for the corp→faction gap) | issue text/comments |

CCP's own help-centre HTML is behind a Cloudflare challenge (HTTP 403 to
non-browser clients); S4/S5 were read through the site's public Zendesk JSON API
instead, which returns the same article body and `edited_at`.

---

## 1. Character standings endpoint (S1, S2)

- **Path:** `GET /characters/{character_id}/standings/`
  (`operationId: GetCharactersCharacterIdStandings`, tag `Character`).
- **OAuth scope:** exactly **`esi-characters.read_standings.v1`**.
  Confirmed both in the operation's `security` block and in the
  `components.securitySchemes.OAuth2.flows.authorizationCode.scopes` map.
  (A separate `esi-corporations.read_standings.v1` exists for corp standings —
  not needed here.)
- **Response shape:** a bare **JSON array** (schema
  `CharactersCharacterIdStandingsGet` is `type: array`). Each item has exactly
  three required fields:

  | field | type | notes |
  |-------|------|-------|
  | `from_id` | int64 | id of the agent / NPC corp / faction |
  | `from_type` | string enum | `agent`, `npc_corp`, `faction` |
  | `standing` | number (double) | the standing value |

  `from_type` enum values are **`agent`, `npc_corp`, `faction`** — note the
  corporation value is `npc_corp`, not `corporation`.
- **Standing value:** a real number on the **−10 … +10** scale (S7: "Standings
  are measured on a real number scale from −10 to +10"). Positive reduces the
  broker fee; negative increases it. The ESI type is `double`, so the value is
  not rounded to 2 dp (this was the explicit ask in esi-issues #277, closed
  noting the ESI endpoint carries the precise value).
- **Pagination:** **none.** It is an unpaginated array; there is no `page`
  parameter. Parameters are only: `character_id`, `Accept-Language`,
  `If-None-Match`, `X-Compatibility-Date`, `X-Tenant`, `If-Modified-Since`.
- **Caching metadata (S1 operation object):**
  - `x-cache-age: 3600`, `x-client-cache-ttl: 3600`,
    `x-server-cache-ttl: 3600`, `x-server-cache-mode: ttl-based`
  - response headers `Cache-Control`, `ETag`, `Last-Modified`
  - rate-limit group `char-social`, `max-tokens: 600`, `window-size: 15m`

  See §5 for refresh guidance.

---

## 2. Resolving the station's owner and its faction

### 2a. Station → owner corporation (S1, S2)

- **Endpoint:** `GET /universe/stations/{station_id}` (public; no OAuth).
- Relevant field: **`owner`** — schema description: *"ID of the corporation that
  controls this station"* (`int64`). Other fields: `name`, `type_id`,
  `system_id`, `position`, `race_id`, `services`, etc.
- Live call for `60004588`: `owner = 1000049` (Brutor Tribe), `system_id =
  30002510`, `name = "Rens VI - Moon 8 - Brutor Tribe Treasury"`. This matches
  the SDE `npcStations.jsonl` record `_key: 60004588`, `ownerID: 1000049`,
  `solarSystemID: 30002510`.

### 2b. Corporation → faction (S1, S2, S3) — **the important gap**

- **Endpoint:** `GET /corporations/{corporation_id}` (public). Its schema
  `CorporationsDetail` **does contain a `faction_id` property**, described as
  *"Corporation's faction ID"*, but it is **not a required field**.
- **Observed live behaviour (S2, 2026-09-21):** ESI returns `faction_id` **only
  for the four faction-warfare militia corporations**, and omits it for every
  other NPC corporation tested:

  | corp id | name | live `faction_id` |
  |---------|------|-------------------|
  | 1000180 | State Protectorate | `500001` |
  | 1000181 | Federal Defense Union | `500004` |
  | 1000182 | Tribal Liberation Force | `500002` |
  | **1000049** | **Brutor Tribe (Rens station owner)** | **absent** |
  | 1000035 | Caldari Navy | absent |
  | 1000051 | Republic Fleet | absent |
  | 1000002 | CBD Corporation | absent |
  | 1000125 | CONCORD | absent |

  This is a long-standing, known limitation: esi-issues **#917** ("Return the
  NPC faction of any NPC corporation", opened 2018 for exactly this broker-fee
  use case, closed 2020-11-12 by the reporter with no fix) and **#457**. The
  only ESI-native workaround documented in #917 is indirect and incomplete:
  `GET /universe/factions` gives one `corporation_id` per faction (the faction's
  main corporation), which cannot map an arbitrary station-owner corp such as
  Brutor Tribe.
- **Resolution that does work:** the faction mapping is in the **SDE**
  (CCP-published, S3). `npcCorporations.jsonl` carries a **`factionID`** field.
  Verified for the station in question:
  - `npcCorporations.jsonl` `_key: 1000049` → `factionID: 500002`
  - `factions.jsonl` `_key: 500002` → `name.en: "Minmatar Republic"`
    (also `corporationID: 1000051`, `militiaCorporationID: 1000182`)
  - (Cross-check: `npcCorporations.jsonl` `_key: 1000035` → `500001`;
    `_key: 1000180` → `500001`.)
- So the full chain for the Rens station is:
  `60004588` → owner `1000049` (Brutor Tribe) → faction `500002`
  (Minmatar Republic). The character's faction standing for the fee is the
  `/characters/{id}/standings/` entry with `from_type == "faction"` and
  `from_id == 500002`; the corp standing is the entry with
  `from_type == "npc_corp"` and `from_id == 1000049`.

**Edge case — NPC corp with no faction (S3).** Of 283 NPC corporations in the
SDE build, 10 have no `factionID`: `1000001` Doomheim, `1000173` Polaris
Corporation, `1000174` Polaris Bug Hunters, `1000175` Polaris Events, `1000176`
Pann's Peeps, `1000197` Templis Dragonaurs, `1000290` Karybdis Infestation,
`1000291` Scylla Infestation, `1000301` Vimoksha Chorus, `1000409` The
Equilibrium of Mankind. Cross-referencing the SDE `npcStations.jsonl` ownerIDs,
**Polaris Corporation (1000173) owns 4 NPC stations**; no other no-faction corp
owns stations in this build. For those stations the faction term cannot be
applied. The Rens station is unaffected.

---

## 3. The broker-fee standings term (S4, S6)

CCP's current support article (S4) states, verbatim (HTML stripped):

> "The broker fee ... Starting at 3% of the order value, the skill 'Broker
> Relations' reduces the fee by 0.3% per level. In addition, increased standings
> with the owner of the NPC station where the order is placed may reduce it by
> up to another 0.2% with maximum standing, and good standings with the owners
> faction can reduce the fee by further 0.3%, to a minimal broker fee of 1% of
> the order value. This results in the following formula to calculate the
> percentage for the broker fee: **3%-(0.3%\*BrokerRelationsLevel)-(0.03%\*FactionStanding)-(0.02%\*CorpStanding)**"

> "The Broker Fees only take unmodified standings into account, so skills that
> increase your effective standing, such as Connections or Diplomacy, do not
> have an effect on these fees."

This **confirms the formula exactly as quoted in the brief**. The EVE
University Trading page (S6) gives the same formula and adds: *"Corporation
standings contribute 2/3 of that of faction standings"* (0.02/0.03), and
*"the unmodified standing is used for the calculation so skills that increase
standings have no effect on broker's fees."*

- **Standing value used:** the **unmodified** (a.k.a. base) standing, on the
  −10…+10 scale — *not* the effective standing after Connections/Criminal
  Connections/Diplomacy. S7 says the same: *"The only confirmed uses of
  unmodified standings are broker fees and faction warfare."*
- **Do faction and corp standdings both apply?** **Yes — additively.** CCP
  (S4) describes the station-owner corp reduction (up to 0.2%) and the
  owner's-faction reduction (a further 0.3%) separately, and the formula sums
  both terms. S6 lists both.
- **Numerical envelope (S4, S6):** no skills/standings → 3%; Broker Relations V
  → 1.5%; plus 10 faction and 10 corp standing → **1.0%**. CCP calls 1% the
  *minimal* broker fee. Note the formula is linear and unbounded on the low
  side of standing, so *negative* standings mathematically raise the fee above
  3%; neither S4 nor S6 states an explicit percentage ceiling — flagged as
  **not explicitly confirmed**.
- **100 ISK minimum (S6):** *"The minimum broker's fee is 100 ISK"* (and the
  minimum relist fee is 100 ISK). S4/S5 do not restate the 100 ISK figure; the
  100 ISK floor is only sourced here from EVE University, not from the CCP
  articles fetched. It is applied **per order** (and per relist), not per unit.

---

## 4. Which standings reduce the fee, and edge cases (S4, S5, S6)

- **Both** the station-owner **corporation** standing and that corporation's
  **faction** standing reduce the fee, additively, per S4/S6 (§3).
- For `60004588`: that means the character's `npc_corp` standing toward
  **Brutor Tribe (1000049)** and their `faction` standing toward **Minmatar
  Republic (500002)**.
- **Missing entries:** ESI returns an array of the standings the character has;
  a character with no standing toward a given entity has no entry, and the
  corresponding term should be treated as **0**. *(Inference from the response
  shape; not explicitly stated by CCP.)*
- **NPC station whose owner has no faction:** no faction term applies (SDE has
  4 such stations; §2b).
- **Player-owned (Upwell) structures — out of scope but noted (S4, S6):**
  broker fee is a fixed `0.5%` SCC surcharge **plus** the structure owner's fee,
  and **is not affected by Broker Relations or by standings** (the owner may set
  its own standing-based fee brackets). Formula: `Broker's fee % = 0.5% + Owner %`
  (S6). Minimum owner fee 0% per the cited patch note in S6; CCP's Upwell
  minimum later moved (devblog mentions a 1% minimum). This is a different
  mechanic and does not use `esi-characters.read_standings.v1` data.

---

## 5. Caching / refresh guidance

From the OpenAPI operation for `/characters/{character_id}/standings/` (S1):

- Server cache: `ttl-based`, `x-server-cache-ttl: 3600s`; `x-cache-age: 3600s`.
- Client guidance: `x-client-cache-ttl: 3600s` — cache the result ~1 hour.
- Conditional requests supported: send `If-None-Match` (ETag) and/or
  `If-Modified-Since` (Last-Modified); both headers are returned on 200.
- Compatibility header available (`X-Compatibility-Date`), default
  `2020-01-01`.
- Rate limit: group `char-social`, 600 tokens / 15 min — not a constraint for
  one character.
- Standings change slowly (mission running, derived standings), so a ~1 h cache
  is safe; there is no websocket/push source for standings.
- The **SDE corp→faction mapping** changes only with game updates, so cache it
  for the life of an SDE build (or download the SDE and store the mapping).

---

## Known uncertainties / not found

- **Does ESI return the *unmodified* standing?** The OpenAPI spec does **not**
  document this, and I found **no CCP statement** that the
  `/characters/{id}/standings/` value is the unmodified rather than the
  Connections/Diplomacy-modified standing. CCP's fee rule (S4) requires the
  unmodified value, and third-party market tools consume this endpoint
  directly, so it is *widely treated* as the unmodified value — but that is an
  **inference, not a cited primary claim**. Recommended verification: compare
  the endpoint value for a test character against their in-game base vs.
  effective standing.
- **Fee ceiling for negative standings:** not stated in S4/S5/S6. The formula
  implies fees above 3% for negative standings; unconfirmed.
- **100 ISK minimum:** sourced only from EVE University (S6); not restated in
  the CCP articles retrieved (S4/S5). CCP's S4 gives instead the "minimal
  broker fee of 1%" (the max-positive-standing floor from the formula).
- **Corp→faction via ESI:** no ESI route returns the faction of an arbitrary
  NPC station owner. `/universe/factions` only maps a faction to its single
  main corporation. This must come from the SDE.
- **Dead ends:** CCP support HTML blocked by Cloudflare (used Zendesk API);
  `support.eveonline.com` Web Archive snapshot is a JS shell; the SDE download
  URL requires a browser `User-Agent` (plain `curl`/default UA gets HTTP 404
  while `HEAD` returns 200); `esi/esi-issues` #917 was closed by the reporter
  without a linked fix.

---

## Decision-relevant summary

To add the standings term for Rens (`60004588`) you need three things:
**(1)** the `esi-characters.read_standings.v1` scope and
`GET /characters/{character_id}/standings/` (array of `{from_id, from_type
∈ {agent,npc_corp,faction}, standing}` on the −10…+10 scale, no pagination,
~1 h cache);
**(2)** the owner corp `1000049` from `GET /universe/stations/60004588`
(`owner` field) and its faction `500002` (Minmatar Republic) — **which ESI
cannot give you**: `/corporations/1000049` omits `faction_id` for all NPC corps
except the 4 FW militia corps. Take it from the **CCP SDE**
`npcCorporations.jsonl` `factionID` field;
**(3)** apply the confirmed formula
`R_b = 3% − 0.3%×BrokerRelations − 0.03%×factionStanding(500002) −
0.02%×corpStanding(1000049)`, using **unmodified** standings, with a per-order
floor of `max(100 ISK, order_value × R_b)`. Both corp and faction terms apply
additively; missing standings count as 0.
