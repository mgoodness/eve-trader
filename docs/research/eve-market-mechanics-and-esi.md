# EVE Online market mechanics and ESI — authoritative reference for a station-trading engine

Question: what are the authoritative EVE Online mechanics and ESI API surfaces
that a station-trading (market-making) recommendation engine must model?

Written: **2026-09-30**. All live HTTP observations below were made on
2026-09-30 against `tranquility`. Every rate/skill number is tagged
**[stated]** (present verbatim in a primary source — cited) or **[inferred]**
(my arithmetic/reasoning connecting stated facts). EVE University is used
only where it restates CCP-published mechanics, and is always cross-checked
against a CCP source; every disagreement between the two is recorded in
[§8](#8-disagreements-between-sources).

Bottom line for the engine:

- Fees/taxes are **two independent charges on different sides**: the *broker
  fee* is charged to whoever **places** a non-immediate order (buy **or** sell),
  on the **total order value**; the *sales tax* is charged to the **seller**
  when an item actually sells.
- NPC-station broker fee = `3% − 0.3%×BrokerRelations − 0.03%×factionStanding −
  0.02%×corpStanding` (floor 1%, min charge 100 ISK). Upwell structures use a
  **fixed** `0.5% SCC + owner%` with **no skill or standing effect**.
- Sales tax = `7.5% × (1 − 0.11×Accounting)` → **3.375% at Accounting V**.
- **Margin Trading no longer exists.** It was renamed to *Advanced Broker
  Relations* on 2020-03-10; a WTB (buy) order now requires **100% escrow up
  front**.
- Order limit = `5 + 4×Trade + 8×Retail + 16×Wholesale + 32×Tycoon` → **305** at
  all V.
- The one route that matters most, region orders, is **cached 300 s** and
  belongs to the **`market-order` rate-limit group (12,000 tokens / 15 min)**.

---

## Method / sources

Primary sources used (fetched 2026-09-30 unless noted):

| Source | URL | Role |
|---|---|---|
| CCP support — Broker Fee and Sales Tax (updated 2026-09-26) | <https://support.eveonline.com/hc/en-us/articles/203218962-Broker-Fee-and-Sales-Tax> | Broker-fee formula + standings, Upwell 0.5% sink, sales tax |
| CCP support — Buy and Sell Orders (updated 2026-09-17) | <https://support.eveonline.com/hc/en-us/articles/203218932-Buy-and-Sell-Orders> | Order matching, relist-fee formula |
| CCP patch notes — March 2020 release | <https://www.eveonline.com/news/view/patch-notes-for-march-2020-release> | Margin Trading → Advanced Broker Relations |
| CCP devblog — Broker Relations | <https://www.eveonline.com/news/view/broker-relations> | "all WTB orders will require 100% escrow" |
| CCP news — The Grand Heist Now Live (2021-07-21) | <https://www.eveonline.com/news/view/the-grand-heist-now-live> | ABR 6%/level → 80% at V |
| CCP patch notes — Version 22.01 (2024-07-25) | <https://www.eveonline.com/news/view/patch-notes-version-22-01> | Sales tax 8% → 4.5% |
| CCP patch notes — Version 22.02 (2025-03-12) | <https://www.eveonline.com/news/view/patch-notes-version-22-02> | Sales tax 4% → 7.5% |
| ESI OpenAPI spec (live) | <https://esi.evetech.net/meta/openapi.json> | Paths, security scopes, cache TTLs, rate-limit groups |
| ESI docs — Rate Limiting | <https://developers.eveonline.com/docs/services/esi/rate-limiting/> | Token-bucket semantics, token costs |
| ESI docs — Best Practices | <https://developers.eveonline.com/docs/services/esi/best-practices/> | Error limit, caching headers |
| ESI docs — X-Pages pagination | <https://developers.eveonline.com/docs/services/esi/pagination/x-pages/> | Page-number pagination |
| ESI blog — Error Rate Limiting Imminent (2017-08-29) | <https://developers.eveonline.com/blog/error-rate-limiting-imminent> | 100 errors / 60 s → HTTP 420 |
| ESI blog — Market Orders rate limit (2026-02-19) | <https://developers.eveonline.com/blog/market-orders-rate-limit-rolls-out-on-february-24-2026> | market-order group 12,000 tokens, 5-min cache |
| ESI docs — Static Data | <https://developers.eveonline.com/docs/services/static-data/> | SDE URLs + build-number flow |
| In-game type text via ESI | `https://esi.evetech.net/latest/universe/types/{type_id}/?language=en` | CCP-authored skill descriptions |
| SDE `types.jsonl`, `mapRegions.jsonl`, `mapSolarSystems.jsonl`, `npcStations.jsonl` | build 3561556 | Type IDs, region/system/station IDs |
| EVE University — Trading | <https://wiki.eveuniversity.org/Trading> | Order-limit formula, worked relist examples |
| EVE University — Skills:Trade | <https://wiki.eveuniversity.org/Skills:Trade> | Per-level skill text + prerequisites |
| EVE University — Upwell structure | <https://wiki.eveuniversity.org/Upwell_structure> | Structure market service context |

CCP sources win over EVE University wherever they disagree.

---

## 1. Broker fee

### When it is charged **[stated]**

- Charged on **creation of any non-immediate buy or sell order**, "based on a
  percentage of the total order value" — CCP support *Broker Fee and Sales Tax*.
  EVE University *Trading* phrases it as "paid every time a sell or buy order
  with duration other than 'immediate' is created."
- Charged **again on modification** (a price change) as a *relist fee* — see
  [§1.4](#14-relist-fee-order-modification).
- **Not refunded** if the order is taken down or expires: "If the order is taken
  down or expires the fee is not paid back." (EVE University *Trading*;
  the CCP support article does not state a refund in either direction.)
- Pays the station owner. In an **Upwell structure**, `0.5%` of the fee is an
  NPC ISK sink and the rest goes to the structure owner (CCP support above).

### Base rate and per-level bonus — NPC stations **[stated]**

CCP support *Broker Fee and Sales Tax*, verbatim:

> Starting at 3% of the order value, the skill "Broker Relations" reduces the
> fee by 0.3% per level. In addition, increased standings with the owner of the
> NPC station where the order is placed may reduce it by up to another 0.2% with
> maximum standing, and good standings with the owners faction can reduce the fee
> by further 0.3%, to a minimal broker fee of 1% of the order value.

Exact formula (CCP support; matches EVE University *Trading*):

```
broker_fee_% = 3% − (0.3% × BrokerRelationsLevel)
                  − (0.03% × FactionStanding)
                  − (0.02% × CorpStanding)
```

- **Affected skill: Broker Relations only** (`type_id 3446`,
  `skill_id` verified from SDE `types.jsonl`). In-game text (ESI
  `/universe/types/3446/`): *"Each level of skill subtracts a flat 0.3% from the
  costs associated with setting up a market order in a non-player station, which
  usually come to 3% of the order's total value."*
- Broker Relations V alone → **1.5%** (EVE University *Trading*; CCP arithmetic).
- Floor: **1%** of order value at max skill + max standings; **minimum charge
  100 ISK** (CCP support gives the 1% floor; EVE University *Trading* gives the
  100 ISK minimum).
- "The Broker Fees only take **unmodified standings** into account, so skills
  that increase your effective standing, such as Connections or Diplomacy, do
  not have an effect on these fees." (CCP support.)

### Upwell (player-owned structure) markets **[stated]**

- "The **Broker Relations skill does not apply** on orders on Upwell
  structures." (CCP support.)
- Structure broker fee = **`0.5%` (SCC surcharge) + owner-set %**; skills do not
  reduce it, though the owner "may set different fees for different standing
  levels" (EVE University *Trading*; the 0.5% NPC split is stated by CCP
  support).
- Owner fee floor: **0%** since Version 21.05, 2023-06-21 (EVE University
  *Trading*, citing the patch notes; the March 2020 devblog *Broker Relations*
  had set the floor to 1% in 2020, so treat 1% as historical).
- Minimum broker fee is 100 ISK in either venue (EVE University *Trading*).

### `Broker Relations` prerequisites **[stated]**

EVE University *Skills:Trade*: prerequisite `Trade II`; at IV it unlocks
*Advanced Broker Relations* (which additionally requires `Accounting IV`).

---

## 2. Sales tax

### Base rate, per-level skill, side **[stated]**

CCP support *Broker Fee and Sales Tax*, verbatim:

> Sales taxes are due after an item has been sold, and are payable by the
> seller. They will be automatically deducted from the sales transaction and
> start at 7.5% of the sales price. This percentage can be reduced down to 3.37%
> through the "Accounting" skill.

CCP in-game text (ESI `/universe/types/16622/`, `Accounting`), verbatim:

> Each level of skill reduces sales tax by 11%. Sales tax starts at 7.5%.

- **Side: seller only.** The buyer of an item pays no sales tax. The wallet
  entry is `transaction_tax` (ESI wallet-journal `ref_type` enum, OpenAPI spec).
- Formula (EVE University *Trading*, consistent with CCP text):

```
sales_tax_% = 7.5% × (1 − 0.11 × AccountingLevel)
```

| Accounting level | Sales tax |
|---|---|
| 0 | 7.5% |
| 1 | 6.675% |
| 2 | 5.85% |
| 3 | 5.025% |
| 4 | 4.2% |
| 5 | **3.375%** (CCP rounds to "3.37%") |

- Sales tax is "always paid to NPC corporation Secure Commerce Commission (SCC)"
  (EVE University *Trading*).
- Prerequisite: `Accounting` requires `Trade IV` (EVE University *Skills:Trade*).

### NPC station vs Upwell market **[stated]**

- The CCP support article describes sales tax once, without an NPC-vs-Upwell
  distinction, and describes the Upwell difference only for the **broker fee**
  (the 0.5% sink + owner cut). No source fetched documents a separate structure
  *sales* tax, so **[inferred]** the SCC sales tax (7.5% base, Accounting-reduced)
  applies the same in an Upwell market; the structure owner is paid through the
  **broker fee**, not the sales tax.
- **Implication for the engine:** skills reduce the fee on **both** venue types
  for sales tax, but only on **NPC stations** for the broker fee. A structure
  trade can therefore be cheaper or more expensive depending purely on the
  owner-set broker fee, not on the trader's own skill/standing.

---

## 3. Buy-order escrow / ISK collateral — and the fate of Margin Trading

### Margin Trading is gone **[stated]**

CCP patch notes, *March 2020 release*, verbatim:

> The **Margin Trading** skill has changed its name and function. It is now
> called **"Advanced Broker Relations"**. This skill will increase the Relist
> Discount rate, from 50% at level 0 (untrained) to 75% at level 5.

CCP devblog *Broker Relations*, verbatim:

> The skill's old ability to create a Want-To-Buy order with only partial ISK
> escrow is removed, so **all WTB orders will require 100% escrow to be paid up
> front.**

- `Margin Trading` is **absent from the current SDE** `types.jsonl` (build
  3561556); `Advanced Broker Relations` is present as `type_id 16597`
  (`Skill:Trade`, group 274). A station-trading engine must **not** model a
  Margin Trading escrow discount.
- `Advanced Broker Relations` has nothing to do with escrow — it only changes
  the relist discount ([§1.4](#14-relist-fee-order-modification)).

### Current escrow mechanics

- Placing a buy order reserves **100% of the order value** in escrow, in
  addition to paying the broker fee **[stated]** (devblog above).
- ESI exposes escrow as a first-class field: `CharacterOrdersGet.escrow` —
  description *"For buy orders, the amount of ISK in escrow"*
  (OpenAPI `components.schemas.CharactersCharacterIdOrdersGet`). It is **not**
  exposed on public region/structure order feeds (those schemas have no
  `escrow` property).
- Wallet escrow movement appears as the `market_escrow` ref_type, with the sale
  itself as `market_transaction`, broker fee as `brokers_fee`, and sales tax as
  `transaction_tax` (OpenAPI wallet-journal `ref_type` enum).
- **[inferred]** The escrowed ISK is returned to the wallet when a buy order is
  cancelled or expires (the devblog only says it must be paid up front; no
  fetched CCP article states the refund sentence explicitly). The broker fee is
  *not* returned on cancel/expiry **[stated]** (EVE University *Trading*).

### Relist fee (order modification) {#14-relist-fee-order-modification}

CCP support *Buy and Sell Orders*, verbatim algorithm:

> BR is the character's effective Broker Fee rate at the station/structure […]
> RD is the character's Relist Discount rate (factoring in Advanced Broker
> Relations skill)
>
> Fee to modify an existing order, changing price from P1 to P2:
> `Fee = max(0, BR (P2 - P1)) + (1 - RD) BR * P2`

- `BR` is the broker-fee rate from [§1](#1-broker-fee).
- **Relist Discount `RD = 50% + 6% × AdvancedBrokerRelationsLevel`** → **80% at
  ABR V**. CCP in-game text (`/universe/types/16597/`) states "adds **6
  percentage points** to the standard Relist Discount of **50%** … permitting a
  discount of **80% at level 5**." CCP's *The Grand Heist Now Live* confirms the
  change: "the Advanced Broker Relations skill will now provide a **6% relist fee
  reduction per level instead of 5%** (max reduction at level 5 will now be
  **80%**)". (The March 2020 launch value was 5%/level → 75% at V; that is
  historical.)
- Minimum relist fee 100 ISK (EVE University *Trading*).
- `Advanced Broker Relations` prerequisites: `Broker Relations IV` **and**
  `Accounting IV` (EVE University *Skills:Trade*).

Worked example from EVE University *Trading* (sell order, 10,000 Tritanium left
at 6.00 ISK, repriced to 7.00 ISK, BROKER 1.88%, ABR IV): relist fee =
`(100%−74%) × 1.88% × 70,000 + 1.88% × 10,000 = 530.16 ISK`.

---

## 4. Order-count and remote-order skills (exact per-level)

All per-level text below is quoted from the CCP-authored in-game type data,
retrieved from ESI `/universe/types/{id}/?language=en` (identical to SDE
`types.jsonl` build 3561556). Prerequisites are from EVE University
*Skills:Trade*.

### 4.1 Order-count skills

| Skill | type_id | Per level | In-game text (verbatim) |
|---|---|---|---|
| Trade | 3443 | **+4** orders | "Active buy/sell order limit increased by 4 per level of skill." |
| Retail | 3444 | **+8** orders | "Each level raises the limit of active orders by 8." |
| Wholesale | 16596 | **+16** orders | "Each level raises the limit of active orders by 16." |
| Tycoon | 18580 | **+32** orders | "Each level raises the limit of active orders by 32." |

Formula and cap **[stated]** (EVE University *Trading*):

```
max_active_orders = 5 + 4×Trade + 8×Retail + 16×Wholesale + 32×Tycoon
```

- Base with **no** trade skills: **5** simultaneous orders.
- All four at V: `5 + 20 + 40 + 80 + 160 =` **305**.
- Prerequisites (EVE University *Skills:Trade*): `Retail` ← Trade II;
  `Wholesale` ← Retail V **and** Marketing II; `Tycoon` ← Wholesale V **and**
  Marketing IV.

### 4.2 Remote-order skills

Range progression is the same "1 system → 5 jumps → double" ladder for all four,
stated in each skill's in-game text.

| Skill | type_id | Function | Range/effect per level |
|---|---|---|---|
| Marketing | 16598 | Place **sell** orders remotely | L1 same solar system; L2 ≤5 jumps; L3 10; L4 20; L5 anywhere in region |
| Procurement | 16594 | Place **buy** orders remotely | L1 same solar system; L2 ≤5 jumps; L3 10; L4 20; L5 anywhere in region |
| Daytrading | 16595 | **Modify** buy/sell orders remotely | L1 same solar system; L2 ≤5 jumps; L3 10; L4 20; L5 anywhere in region |
| Visibility | 3447 | Sets the **effective range of a remote buy order** | L1 same system; L2 ≤5 jumps; L3 10; L4 20; L5 full region |

Verbatim anchors:

- Marketing: *"Level 1 allows for the sale of items within the same solar
  system, Level 2 extends that range to systems within 5 jumps, and each
  subsequent level then doubles it. Level 5 allows for sale of items located
  anywhere within current region."*
- Procurement: *"Level 1 allows for the placement of orders within the same
  solar system, Level 2 extends that range to systems within 5 jumps, and each
  subsequent level then doubles it. Level 5 allows for placement of remote buy
  orders anywhere within current region."*
- Daytrading: *"Level 1 allows for modification of orders within the same solar
  system, Level 2 extends that range to systems within 5 jumps, and each
  subsequent level then doubles it. Level 5 allows for market order modification
  anywhere within current region."*
- Visibility: *"Only remotely placed buy orders (using Procurement) require this
  skill to alter the range. Any range can be set on a local buy order with no
  skill."*

**[inferred] engine rules:** if the trader is docked in the *same station* as
the goods, Marketing/Procurement/Visibility are not needed. Procurement gates
*whether* a buy order may be placed remotely; Visibility sets the buy order's
maximum *range in jumps*; Marketing/Procurement's own per-level range gates how
far the *placement* may happen. Daytrading gates remote *modification*.

Prerequisites (EVE University *Skills:Trade*): `Marketing` ← Trade II;
`Procurement` ← Marketing II; `Visibility` ← Procurement IV; `Daytrading` ←
Trade IV.

---

## 5. Standings effects on fees

**[stated]** CCP support *Broker Fee and Sales Tax*:

- Standings affect the **NPC-station broker fee only**.
- Faction standing weight: **0.03% per standing point, up to 0.3%** at max.
- Corporation standing weight: **0.02% per standing point, up to 0.2%** at max
  (i.e. corporation standing is 2/3 of faction — EVE University *Trading*).
- Combined floor with Broker Relations V: `3% − 1.5% − 0.5% =` **1.0%**.
- Only **unmodified** standings count; social skills (Connections, Diplomacy)
  do not change the fee.
- Standings do **not** affect sales tax (no source states any such effect).
- Standings do **not** affect the Upwell broker fee directly, but the structure
  owner may set different owner fees for different standing levels (EVE
  University *Trading*).

For the engine, standings are readable from
`GET /latest/characters/{character_id}/standings/` (`from_type` ∈ `agent`,
`npc_corp`, `faction`; `standing` is a double) — OpenAPI
`CharactersCharacterIdStandingsGet`.

---

## 6. ESI endpoints for a station-trading tool

Everything below is extracted from the live OpenAPI spec
(<https://esi.evetech.net/meta/openapi.json>, fetched 2026-09-30) and confirmed
against live response headers. Paths are shown with the stable `/latest/` alias;
the spec itself lists them without a version prefix, and `vN` aliases also
resolve. Add an `X-Compatibility-Date` header per the ESI overview (default
`2020-01-01`; the live `x-compatibility-date` header is `2020-01-01`).

### 6.1 Region market orders (public) — the primary feed

```
GET https://esi.evetech.net/latest/markets/{region_id}/orders/
    ?order_type=all            # required; enum: buy | sell | all (default all)
    &type_id={type_id}         # optional filter
    &page={n}                  # optional, minimum 1
```

- **Auth/scope:** none (`security: None`).
- **Pagination:** page-number, `X-Pages` response header; **1,000 orders per page**
  (observed: 1,000 items on page 1 for region 10000030, `X-Pages: 71`).
- **Caching:** `x-server-cache-ttl = 300` s, `x-client-cache-ttl = 300` s,
  mode `ttl-based` (OpenAPI `x-*` extensions). Live header
  `expires` = `last-modified + 300 s`. The ESI blog calls this out explicitly:
  "the route is cached for 5 minutes".
- **Rate limit:** group **`market-order`**, **12,000 tokens / 15 min**
  (`x-rate-limit` extension; live header `x-ratelimit-limit: 12000/15m`).
- **Order record fields:** `order_id, type_id, location_id, system_id,
  volume_total, volume_remain, min_volume, price, is_buy_order, duration,
  issued, range`. `range` enum: `station, solarsystem, region, 1, 2, 3, 4, 5,
  10, 20, 30, 40`. No `escrow` field (public feed).

### 6.2 Market history (public)

```
GET https://esi.evetech.net/latest/markets/{region_id}/history/?type_id={type_id}
```

- **Auth/scope:** none.
- **Params:** `type_id` is **required**.
- **Pagination:** none (single array).
- **Expiry:** OpenAPI description — *"This route expires daily at 11:05"*
  (live header `expires` = next day 11:05 UTC; `last-modified` = that day's
  11:05).
- **Rate limit:** no `x-rate-limit` group → subject only to the error limit
  ([§6.6](#66-rate-limits--caching--error-limit)).
- **Record fields:** `date, order_count, volume, highest, average, lowest`.
  Observed 425 daily entries for Tritanium in Heimatar (2025-08-01 →
  2026-09-29); no documented maximum was found.

### 6.3 Structure (Upwell) market orders

```
GET https://esi.evetech.net/latest/markets/structures/{structure_id}/?page={n}
```

- **Auth/scope:** OAuth2 **`esi-markets.structure_markets.v1`**.
- **Pagination:** page-number, `X-Pages`; same 1,000-row page shape.
- **Caching:** `x-server-cache-ttl = 300` s, `x-client-cache-ttl = 300` s.
- **Rate limit:** no `x-rate-limit` group on this route (error-limit only).
- **Fields:** same as region orders **minus** `system_id` (`MarketsStructuresStructureIdGet`
  has no `system_id`).
- The OpenAPI route description is only *"Return all orders in a structure"*;
  it does **not** state the docking-access requirement. **Not found**: no
  first-party sentence in the fetched ESI docs/blog describing the docking gate
  on this route — treat any docking requirement as unverified here.

### 6.4 Character skills (needed for every fee/slot/range calculation)

```
GET https://esi.evetech.net/latest/characters/{character_id}/skills/
```

- **Auth/scope:** OAuth2 **`esi-skills.read_skills.v1`**.
- **Caching:** `x-client-cache-ttl = 60` s; `x-server-cache-mode = event-based`.
- **Rate limit:** group **`char-detail`**, **600 tokens / 15 min**.
- **Pagination:** none.
- **Shape:** `{ total_sp, unallocated_sp?, skills:[{ skill_id,
  trained_skill_level, active_skill_level, skillpoints_in_skill }] }`.
  Use **`active_skill_level`** (not `trained_skill_level`): it "can differ from
  trained due to alpha status and/or active expert systems."
- **Caveat (verbatim from OpenAPI):** *"Skills returned by this route can be
  out-of-date if the character hasn't logged in since one or more skills
  completed training."* Combine with
  `GET /latest/characters/{character_id}/skillqueue/`
  (`esi-skills.read_skillqueue.v1`, 60 s, `char-detail`) for accuracy.

### 6.5 Adjacent character endpoints

| Purpose | Path | Scope | Cache | Rate group |
|---|---|---|---|---|
| Open orders (incl. `escrow`) | `/latest/characters/{character_id}/orders/` | `esi-markets.read_character_orders.v1` | 1200 s | none (error-limit only) |
| Order history | `/latest/characters/{character_id}/orders/history/` | `esi-markets.read_character_orders.v1` | 3600 s | none |
| Standings | `/latest/characters/{character_id}/standings/` | `esi-characters.read_standings.v1` | 3600 s | `char-social` (600/15m) |
| Wallet balance | `/latest/characters/{character_id}/wallet/` | `esi-wallet.read_character_wallet.v1` | 120 s | `char-wallet` (150/15m) |
| Wallet journal (fee/tax/escrow audit) | `/latest/characters/{character_id}/wallet/journal/` | `esi-wallet.read_character_wallet.v1` | 3600 s | `char-wallet` (150/15m) |
| Corporation open orders | `/latest/corporations/{corporation_id}/orders/` | `esi-markets.read_corporation_orders.v1` + roles `Accountant`/`Trader` | 1200 s | none |

### 6.6 Rate limits / caching / error limit

Two **independent** limiters apply (ESI docs, Rate Limiting and Best Practices):

1. **Bucket (floating-window token) limit.** Only on routes that declare
   `x-rate-limit`. Per `(rate-limit-group, userID)` bucket; for unauthenticated
   routes `userID = sourceIP` (`sourceIP:applicationID` if a token is sent).
   Token cost per request: **2XX = 2 tokens, 3XX = 1, 4XX = 5 (429 excluded),
   5XX = 0**. Headers: `X-Ratelimit-Group`, `X-Ratelimit-Limit` (e.g.
   `12000/15m`), `X-Ratelimit-Remaining`, `X-Ratelimit-Used`; on 429 a
   `Retry-After` in seconds.
2. **Error limit (all routes).** *"at most 100 non-2xx/3xx responses per minute.
   After that, it will return 420s on all ESI routes."* Headers
   `X-ESI-Error-Limit-Remain`, `X-ESI-Error-Limit-Reset`. The 2017 blog states
   the target was "100 allowed errors per 60 second windows." The two header
   sets are mutually exclusive per response.

Route-specific limits confirmed live/from the spec:

| Route group | Limit |
|---|---|
| `market-order` (`/markets/{region_id}/orders`) | 12,000 tokens / 15 min |
| `char-detail` (skills, attributes, implants, …) | 600 / 15 min |
| `char-social` (standings, contacts, …) | 600 / 15 min |
| `char-wallet` (wallet, journal, transactions) | 150 / 15 min |
| `routes` (`/route/…`) | 3,600 / 15 min |
| error limit (global) | 100 non-2xx/3xx / 60 s → 420 |

**Caching discipline:** do not request before `expires`; honour `ETag`
(`If-None-Match`) and `Last-Modified` (`If-Modified-Since`) — a `304` costs only
1 token and the docs explicitly warn that circumventing ESI caching can get an
app banned (Best Practices). For `X-Pages` resources, refetch all pages if page 1
is near cache expiry, to avoid duplicated/missed rows (ESI X-Pages doc).

**Practical budget:** the region-orders feed at 300 s cache over Heimatar alone
(71 pages observed) costs `71 × 2 = 142` tokens per refresh, ×3 refreshes per
15-min window ≈ **426 tokens** — trivially inside the 12,000 budget. Budget
pressure comes from fanning out to many regions, not from one trade hub.

### 6.7 Supporting public endpoints (no auth)

| Purpose | Path | Cache |
|---|---|---|
| Type metadata + CCP skill text | `/latest/universe/types/{type_id}/` | — |
| Resolve IDs → names | `POST /latest/universe/names/` | — |
| Resolve names → IDs | `POST /latest/universe/ids/` | — |
| Region info | `/latest/universe/regions/{region_id}/` | — |
| System info | `/latest/universe/systems/{system_id}/` | — |
| NPC station info (name, services) | `/latest/universe/stations/{station_id}/` | — |
| Jump route (for remote-order range math) | `/latest/route/{origin}/{destination}/` | 86,400 s; group `routes` (3,600/15m) |
| Type IDs with active orders in a region | `/latest/markets/{region_id}/types/` | 600 s |
| Reference "adjusted/average" prices | `/latest/markets/prices/` | 3600 s |

---

## 7. Static data (SDE) and concrete IDs

### 7.1 Where the SDE comes from **[stated]**

Per the ESI Static Data doc (<https://developers.eveonline.com/docs/services/static-data/>):

- Latest build number: `GET https://developers.eveonline.com/static-data/tranquility/latest.jsonl`
  → returns the `sde` record. **Observed 2026-09-30:**
  `{"_key":"sde","buildNumber":3561556,"releaseDate":"2026-09-30T11:07:52Z"}`.
- Data archive:
  `https://developers.eveonline.com/static-data/tranquility/eve-online-static-data-<build>-jsonl.zip`
  (short-hand: `.../static-data/eve-online-static-data-latest-jsonl.zip`). Build
  3561556 JSONL zip = **99,197,188 bytes**, 102 files, ~579 MB uncompressed.
- Relevant files: `types.jsonl`, `mapRegions.jsonl`, `mapSolarSystems.jsonl`,
  `npcStations.jsonl` (also `stationOperations.jsonl`, `stationServices.jsonl`,
  `marketGroups.jsonl`, `groups.jsonl`). `npcStations.jsonl` **does not carry
  station names** — only `solarSystemID`, `ownerID`, `typeID`, `operationID`,
  etc. Names come from `GET /universe/stations/{id}/`.
- Schema changes are published at
  `https://developers.eveonline.com/static-data/tranquility/schema-changelog.yaml`.

### 7.2 Heimatar / Rens IDs **[stated — ESI + SDE, verified 2026-09-30]**

| Entity | ID | Source |
|---|---|---|
| Region **Heimatar** | `10000030` (faction `500002` = Minmatar Republic; constellation IDs 20000367–20000378) | ESI `/universe/regions/10000030/`; SDE `mapRegions.jsonl` |
| System **Rens** | `30002510` (`regionID 10000030`, `hub: true`, `border: true`, constellation `20000367`) | ESI `POST /universe/ids/`; SDE `mapSolarSystems.jsonl` |
| Trade-hub station **Rens VI − Moon 8 − Brutor Tribe Treasury** | `60004588` (system 30002510, owner corp `1000049` Brutor Tribe, has `market` service) | ESI `/universe/stations/60004588/`; station exists in SDE `npcStations.jsonl` |

Other market stations in Rens (system 30002510): `60004594` Rens VII − Moon 17 −
Brutor Tribe Bureau; `60005725` Rens VI − Six Kin Development Warehouse;
`60009106` Rens VIII − Moon 3 − TransStellar Shipping Storage; `60012721`,
`60012724`, `60012727` (Sisters of EVE Bureau); `60015172` Rens V − Paragon
Fulfillment Center.

Note: `30002510` is the **system** id; do not confuse it with the commonly
quoted `30002526`, which is a different system.

### 7.3 Item type IDs **[stated — ESI `/universe/ids/`, 2026-09-30]**

| Item | type_id |
|---|---|
| Tritanium | `34` |
| Pyerite | `35` |
| Mexallon | `36` |
| Isogen | `37` |
| Nocxium | `38` |
| Zydrine | `39` |
| Megacyte | `40` |
| Morphite | `11399` |
| PLEX | `44992` |
| Large Skill Injector | `40520` |

### 7.4 Skill type IDs **[stated — SDE `types.jsonl`, build 3561556]**

| Skill | type_id |
|---|---|
| Trade | `3443` |
| Retail | `3444` |
| Broker Relations | `3446` |
| Visibility | `3447` |
| Procurement | `16594` |
| Daytrading | `16595` |
| Wholesale | `16596` |
| Advanced Broker Relations | `16597` |
| Marketing | `16598` |
| Accounting | `16622` |
| Tycoon | `18580` |

(`Margin Trading` is not present in the current SDE.)

---

## 8. Disagreements between sources

1. **Sales-tax base: 7.5% (CCP) vs stale 8% (EVE University bullet).**
   EVE University *Trading* contains two different numbers: the "Skills" summary
   bullet says Accounting "reduces the sales tax by 11% per level **from 8% to
   3.6% at level 5**", while its own Tax section and CCP support say **7.5% →
   3.37%**. CCP support (updated 2026-09-26), CCP in-game text
   (`/universe/types/16622/`), and the Version 22.02 patch note ("Sales Tax has
   been increased from 4% to 7.5%") all agree on **7.5%**. **Winner: 7.5% /
   3.375% at Accounting V.**
2. **Advanced Broker Relations: 6 pp/level (current CCP) vs 5 pp/level (stale
   EVE University bullet and 2020 launch text).** EVE University *Trading*'s
   summary bullet says "5 percentage points per level (10% reduction per level)",
   but its own detailed formula and the CCP in-game text use **6 pp → 80% at V**;
   CCP *The Grand Heist Now Live* confirms "6% ... instead of 5% ... 80%".
   **Winner: 6 pp/level, 80% at ABR V.**
3. **EVE University summary "5.1% … 11% of the order price" (fees/taxes).**
   This sentence is stale: 3% broker + 8% sales tax = 11%, and 1.5% + 3.6% =
   5.1%, i.e. it uses the **old 8%** sales tax and counts **only one broker fee
   (the sell order)**. With the current 7.5% base and counting both broker fees,
   the zero-standing/max-skill round-trip cost is higher. **Winner: derive from
   the CCP per-component formulas, not this summary.**
4. **Buy-order escrow and Margin Trading.** The task brief frames escrow around
   a *Margin Trading* skill; CCP sources state that skill was **removed/renamed
   on 2020-03-10** and that buy orders now require **100% escrow**. **Winner:
   CCP patch notes/devblog.** No fetched historical EVE University page
   contradicting this survives into the current text.
5. **Upwell owner-fee floor: 1% (March 2020) vs 0% (current).** The March 2020
   devblog raised the floor to 1%; EVE University *Trading* cites Version 21.05
   (2023-06-21) lowering it to 0%. Use **0%** unless an older compatibility date
   is pinned.

---

## 9. What the engine should compute (all derived from the cited rates)

For a completed NPC-station round trip (place buy order → filled → place sell
order → sold), cash costs are:

```
buy-side broker fee  = broker_fee_% × (buy order value)      # paid at buy-order creation
sell-side broker fee = broker_fee_% × (sell order value)     # paid at sell-order creation
sales tax            = sales_tax_%  × (sell order value)     # paid when the sell order fills
```

plus, for any price modification, `relist_fee` from
[§3](#3-buy-order-escrow--isk-collateral--and-the-fate-of-margin-trading), and a
one-time **100% escrow lock** of the buy-order value at buy-order creation.

At **zero standings / max skills / NPC station** the marginal rates are
`broker_fee_% = 1.5%` and `sales_tax_% = 3.375%`. At **max standings** the
broker rate is `1.0%`. In an **Upwell** market replace `broker_fee_%` with the
structure's `0.5% + owner%` (skill/standing independent), keeping the same
`sales_tax_%` **[inferred]**. The recommendation filter must therefore clear at
least `broker(buy) + broker(sell) + sales tax` on the round trip before showing a
positive net margin.

---

## Open questions / not found

- **Buy-order escrow refund sentence.** CCP states 100% escrow up front, and ESI
  models `market_escrow`, but no fetched first-party article states in one
  sentence that unused escrow is returned on cancel/expiry. It is inferred from
  the mechanics; worth confirming against a live wallet journal
  (`market_escrow` ref_type) if the engine surfaces "locked ISK".
- **Structure-market docking gate.** The `esi-markets.structure_markets.v1`
  scope is confirmed from OpenAPI, but no fetched first-party document states
  the docking-access requirement for `GET /markets/structures/{structure_id}/`.
- **Market-history retention.** No documented maximum number of days; 425 days
  observed for one type in Heimatar. Do not assume a fixed window.
- **Structure owner-fee UI value.** ESI does not expose a structure's configured
  broker-fee percentage; the engine cannot compute an Upwell broker fee from ESI
  alone and must ask the user or read it in-client. (Not found in the ESI spec.)
- **Sales tax in Upwell markets.** No source found that treats structure sales
  tax differently from the SCC sales tax; the [§2](#2-sales-tax) "same rate"
  conclusion is inferred from the absence of a stated difference, not from a
  positive statement.
