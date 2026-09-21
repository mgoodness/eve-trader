# ESI endpoints and scopes for character market-activity tracking & realized P/L

Research date: 2026-09-21 (UTC). Repo default branch assumed: n/a (this is an
external-API question; no repo version pin applies).

## Sources (primary)

- **ESI OpenAPI spec (authoritative for routes, fields, scopes, cache, rate
  limits)** — `https://esi.evetech.net/meta/openapi.json`, retrieved 2026-09-21.
  Title `EVE SKINR Ingenuity (ESI) - tranquility`, `info.version` `2020-01-01`.
  This is the spec the API Explorer at `https://esi.evetech.net/ui/` serves.
  Individual route compat date for the wallet/orders/contracts routes is
  `2020-01-01` unless noted.
- **ESI docs repo (official)** — `https://github.com/esi/esi-docs`
  (`docs/services/esi/...`). Contains pagination, caching, rate-limit docs.
- **ESI issue tracker (official, `esi/esi-issues`)** — bug/feature reports with
  CCP (ESI team) responses; used only where the spec is silent, and labelled as
  such.
- **`esi/eve-glue`** — the library the ESI changelog explicitly cites for the
  wallet-journal `ref_type` enum:
  `https://github.com/esi/eve-glue/blob/master/eve_glue/wallet_journal_ref.py`
  (linked from `GET /meta/changelog`, entry `2025-07-21`). Used for the
  numeric `ref_type` IDs and the `context_id`/argument mapping.
- **ESI changelog** — `https://esi.evetech.net/meta/changelog`.
- **EVE Developer dev blogs** — `https://developers.eveonline.com/blog/...`
  (caching, market-order rate limit).

Anything not backed by one of the above is explicitly marked **inference** or
**uncertain**.

---

## Decision-relevant summary (read this first)

1. **A relist chain is NOT directly reconstructable.** The order-history route
   returns only `state: "cancelled" | "expired"`; there is no "replaces"/parent
   order id, and `brokers_fee` journal entries carry **no order reference**
   (feature request `esi/esi-issues#1369` is still **open** as of 2026-09-21).
   You can only *infer* a relist by matching `type_id` + `location_id` (+
   approximate time and possibly `price`) across a `cancelled` history order and
   a later open/history order. Fully-filled orders vanish from both endpoints
   (`esi/esi-issues#612`), so the chain can have gaps.
2. **An on-behalf-of transfer via an item-exchange contract IS detectable**, but
   only if you read the *contracts* routes, not the wallet alone: `GET
   /characters/{id}/contracts` gives `type:"item_exchange"`, the counterparty
   (`assignee_id`/`acceptor_id`) and the ISK `price`, and `GET
   /characters/{id}/contracts/{contract_id}/items` gives the exact `type_id` /
   `quantity` / `is_included` that left the character. The wallet journal only
   shows a `contract_price` entry **when there is a non-zero price**; a
   zero-price "gift" contract produces no ISK movement.
3. **A direct player-to-player trade window transfer is NOT observable at all.**
   There is no ESI trade endpoint (no `/characters/{id}/trades` in the spec), and
   item movement via the trade window produces no wallet journal entry. This is a
   genuine blind spot; only the ISK side signs of `player_trading` /
   `player_donation` exist.

---

## 1. Exact OAuth scope strings

Taken verbatim from `components.securitySchemes.OAuth2.flows.authorizationCode.scopes`
in the ESI spec, and confirmed against each route's `security` block.

| Data | Personal (character) | Corporation |
|---|---|---|
| Wallet transactions | `esi-wallet.read_character_wallet.v1` | `esi-wallet.read_corporation_wallets.v1` |
| Wallet journal | `esi-wallet.read_character_wallet.v1` | `esi-wallet.read_corporation_wallets.v1` |
| Open market orders | `esi-markets.read_character_orders.v1` | `esi-markets.read_corporation_orders.v1` |
| Order history | `esi-markets.read_character_orders.v1` | `esi-markets.read_corporation_orders.v1` |
| Contracts (incl. item exchange + contract items) | `esi-contracts.read_character_contracts.v1` | `esi-contracts.read_corporation_contracts.v1` |
| Assets (needed for inventory checks) | `esi-assets.read_assets.v1` | `esi-assets.read_corporation_assets.v1` |

Notes:
- Both `wallet/transactions` and `wallet/journal` share one scope
  (`esi-wallet.read_character_wallet.v1`); there is no separate scope per route.
- The contract-items route
  (`GET /characters/{id}/contracts/{contract_id}/items`) also uses
  `esi-contracts.read_character_contracts.v1` — no extra scope.
- Scopes are requested as a **space-separated** list on the SSO authorize
  request (`scope=<space-separated list of scopes>`), per
  `esi-docs/docs/services/sso/index.md`.
- Wallet/orders scopes are **personal-scoped**; corp and personal data are not
  readable with one scope. A single-character tracker needs exactly:
  `esi-wallet.read_character_wallet.v1`, `esi-markets.read_character_orders.v1`,
  `esi-contracts.read_character_contracts.v1`, `esi-assets.read_assets.v1`.

---

## 2. `GET /characters/{id}/wallet/transactions/`

Spec path: `/characters/{character_id}/wallet/transactions` (`GET`).
Summary/description: `"Get wallet transactions of a character"` — the current
spec states **no retention window**.

### Response fields (all required per current spec)

| Field | Type | Spec description | Notes / semantics |
|---|---|---|---|
| `transaction_id` | int64 | Unique transaction ID | Primary key; sort/backfill key. |
| `date` | date-time | Date and time of transaction | |
| `location_id` | int64 | *(no description)* | Station/structure of the trade. |
| `type_id` | int64 | *(no description)* | Item type. |
| `unit_price` | double | "Amount paid per unit" | |
| `quantity` | int64 | *(no description)* | Units. Gross value = `unit_price * quantity`. |
| `client_id` | int64 | *(no description)* | **Counterparty.** |
| `is_buy` | boolean | *(no description)* | `true` = the character bought (ISK out). |
| `is_personal` | boolean | *(no description)* | See below. |
| `journal_ref_id` | int64 | *(no description)* | Id of the related wallet journal entry (intended linkage). |

### `client_id` — is it the counterparty?

**Yes — it is the other party of the trade**, but the spec does **not** document
it. Evidence:
- `esi/esi-issues#719` treats `client_id` as "the target" in transactions and
  notes it may be a **character, corporation, or alliance ID**, requiring
  `POST /universe/names/` to resolve its type/name.
- `esi/esi-issues#989` shows a corp transaction with `client_id` populated and
  argues the *acting character* is missing — i.e. `client_id` is the
  counterparty, not the actor.
- **Uncertain**: NPC counterparties can appear (e.g. `first_party_id` 1000132
  for CONCORD-adjacent rows in journal examples). Also `client_id` is not
  guaranteed to be a character.

Resolution: resolve all non-null `client_id` values through
`POST /universe/names/` (accepts up to 1000 ids; returns character/corp/alliance
names).

### `is_personal`

No spec description. `esi/esi-issues#539` establishes (from CCP triage) that the
character transactions route also returns transactions made using the
**corporation wallet**, and that `is_personal` is the flag distinguishing the
character's personal-wallet transactions (`true`) from corp-wallet ones
(`false`). **Uncertain**: exact semantics for every edge case are not documented.

### `journal_ref_id`

No spec description. Intended to link a transaction to its wallet journal entry.
**Caution**: `esi/esi-issues#991` (still open) reports that for the *corporation*
transactions route `journal_ref_id` does not match any journal `id`. For the
*character* route the more robust linkage is the journal side: match
`journal.context_id` where `journal.context_id_type == "market_transaction_id"`
against `transaction.transaction_id` (CCP-suggested workaround in the #991
thread; see §3).

### Retention / pagination / backfill

- **Pagination/backfill is `from_id` only** — there is **no `page` parameter** on
  this route in the current spec. Query param:
  `from_id` — *"Only show transactions happened before the one referenced by this id"*.
- `from-id` pagination is documented in
  `esi-docs/docs/services/esi/pagination/from-id.md`: initial request without
  `from_id` returns the most recent records; then pass the last
  `transaction_id`; the response **always includes the `from_id` record**, so
  the documented stop condition is "if the response contains only the `from_id`
  record, stop." (This inclusive behaviour is also reported as a long-standing
  quirk in `esi/esi-issues#1301`, `#715`, `#553`.)
- **Retention window**: **not documented in the current spec.** Historical
  evidence only:
  - The now-deprecated swagger (`esi/esi-swagger-specs`,
    `latest/swagger.json` / `dev/swagger.json`) declared
    `responses.200.schema.maxItems: 2500` for this route, and `esi/esi-issues#489`
    (CCP reply, 2017) confirms "an upper limit of 2500 transactions it can
    return". The current OpenAPI spec has **dropped `maxItems`** entirely (no
    `2500` string anywhere in `meta/openapi.json`), so it is **uncertain** whether
    the 2500 cap still applies.
  - `esi/esi-issues#731` (closed 2020-02-04) is the key thread: a CCP reply
    ("Xamber") said the docs clarification *"will be shipped with v2 versions of
    the transaction endpoints."* The live service now exposes
    `/v2/characters/{id}/wallet/transactions/` (checked: returns `401` without a
    token, i.e. route exists), but the **current spec still does not state a
    retention window** for transactions (unlike journal, which says 30 days).
  - **Conclusion / labelled uncertain**: treat transaction retention as
    *undocumented*. The spec's own from-id guide frames the route as the
    historical-collection route, but do not rely on >30-day backfill without
    testing. Poll and persist on your side rather than re-fetching history.
- **Cache**: `x-cache-age` / `x-server-cache-ttl` = **3600 s** (1 h),
  `x-server-cache-mode: ttl-based`.
- **Rate limit**: group `char-wallet`, `150 / 15m`.

---

## 3. `GET /characters/{id}/wallet/journal/`

Spec path: `/characters/{character_id}/wallet/journal` (`GET`).
Description: **"Retrieve the given character's wallet journal going 30 days
back"** — this is the documented retention limit.

### Fields

`amount` (signed; positive = deposit, negative = withdrawal), `balance`,
`context_id`, `context_id_type`, `date`, `description`, `first_party_id`, `id`
(unique journal ref id), `reason`, `ref_type`, `second_party_id`, `tax`,
`tax_receiver_id`. Only `date`, `id`, `ref_type`, `description` are required.

`context_id_type` enum includes `market_transaction_id`, `contract_id`,
`structure_id`, `station_id`, `character_id`, `corporation_id`, `alliance_id`,
`type_id`, others. The spec warns `context_id` *"means different things"* per
`ref_type` and may be absent.

### `ref_type` values relevant to market activity

The spec only lists the enum; it does **not** document per-value semantics. The
numeric IDs and argument mapping come from `esi/eve-glue`
(`eve_glue/wallet_journal_ref.py`), which the ESI changelog cites as the backing
source for this enum (changelog entry `2025-07-21` links
`https://github.com/esi/eve-glue/pull/44`).

| Meaning | `ref_type` string | eve-glue id | Linkage (`context_id`) |
|---|---|---|---|
| Market transaction settlement | `market_transaction` | 2 | `context_id = transaction_id`, `context_id_type = market_transaction_id` (eve-glue maps ref 2 → `transaction_id`). |
| Broker fee (order placement/modification/relist) | `brokers_fee` | 46 | **None.** eve-glue deliberately excludes 46 from the argument mapping; `esi/esi-issues#1369` is an **open** request to add `context_id` here. |
| Sales tax on a market sale | `transaction_tax` | 54 | **None.** Same: excluded in eve-glue; covered by open `#1369`. |
| Market escrow (deposit/release) | `market_escrow` | 42 | Historically `context_id_type = market_transaction_id`, but `esi/esi-issues#958` documented wrong values (=1); that issue was **closed 2025-07-16** as self-resolved. `esi/esi-issues#1456` (open) reports the `description` text differs from the client. Treat as best-effort. |
| Market fine | `market_fine_paid` | 44 | — |
| Structure "provider tax" | `market_provider_tax` | 149 | Added in journal enum update. |
| Security tax | `market_security_tax` | 194 | Added in journal enum update. |
| Contract price (item-exchange sale, personal) | `contract_price` | 71 | `context_id_type = contract_id` (eve-glue maps contract refs 63–84 to `contract_id`). |
| Contract price paid from corp wallet | `contract_price_payment_corp` | 79 | `context_id_type = contract_id`. |
| Contract broker fee / sales tax | `contract_brokers_fee` (72), `contract_brokers_fee_corp` (80), `contract_sales_tax` (73), `contract_deposit_sales_tax` (75) | — | `context_id_type = contract_id`. |
| Contract collateral/deposit/reward group | `contract_collateral*`, `contract_deposit*`, `contract_reward*`, `contract_auction_*`, `contract_reversal` | 63–84 | `context_id_type = contract_id`. |
| Direct ISK player transfer | `player_trading` (1), `player_donation` (10) | — | `player_trading` maps arg → `location_id`. Items are **not** represented. |

**Fee attribution to a specific order/item**: For `market_transaction`, you get
`context_id == transaction_id`, which lets you join back to
`/wallet/transactions` (and thus to `type_id`, `quantity`, `unit_price`,
`location_id`). For `brokers_fee` and `transaction_tax` there is **currently no
join key** (open `#1369`) — attribution must be heuristic (amount, timestamp,
venue, and the pending order list). This is the single biggest limitation for
exact relist-cost accounting.

### Retention & backfill

- **30 days** (documented in the route description). The same limit is asserted
  for corp journals in `esi/esi-issues#1172`; CCP there: *"This is a hardcoded
  constraint that applies to the client as well. ESI cannot offer any advantages
  over the desktop client."*
- **Pagination**: `page` query param, `X-Pages` response header
  (`esi-docs/docs/services/esi/pagination/x-pages.md`). Pages are 1-indexed.
  `esi/esi-issues#1172` observed **1000 records/page** for the corp journal;
  the char journal page size is not stated in the spec. The deprecated swagger
  declared `maxItems: 2500`; current spec does not.
- **Cache**: 3600 s. **Rate limit**: group `char-wallet`, `150 / 15m`.

---

## 4. Open orders and order history

### `GET /characters/{id}/orders/` (open)

Description: *"List open market orders placed by a character"*.
Fields: `order_id`, `type_id`, `region_id`, `location_id`, `range`, `price`,
`volume_total`, `volume_remain`, `issued`, `duration`, `is_buy_order`,
`is_corporation`, `escrow`, `min_volume`. **No `state` field** on the open
route; **no fills list**. `escrow` is documented as "for buy orders, the amount
of ISK in escrow".

Cache `x-server-cache-ttl` = **1200 s** (20 min). `x-rate-limit` is **null** in
the current spec (not yet in a bucket group).

### `GET /characters/{id}/orders/history/`

Description: **"List cancelled and expired market orders placed by a character
up to 90 days in the past."**
Fields: same as open **plus** `state`, and **without** a delta — `state` enum is
**exactly `["cancelled", "expired"]`**. Required includes `state`,
`volume_total`, `volume_remain`, `issued`.

Key consequences:
- **Fully-filled orders are absent from both routes.** `esi/esi-issues#612`
  (CCP reply 2018-01-25): the history route was added to expose "cancelled or
  expired" orders going 90 days back, and open orders were made v2/dedicated to
  open only. Orders completed immediately / fully filled are never returned.
- **Fill inference**: for a history row, filled quantity can only be inferred as
  `volume_total - volume_remain` (the remaining amount at cancellation/expiry).
  There is **no per-fill record and no fill timestamp in either orders route**.
  Per-fill detail must come from `market_transaction` journal entries (and their
  linked transactions).
- **Relist reconstruction**: there is **no parent/replacement id**. A relist =
  a `cancelled` row plus a later (open or history) row. You can only join on
  `type_id` + `location_id` (and optionally `price`, `issued` ordering). The
  relist's extra broker fee is a separate `brokers_fee` journal entry with no
  order id (§3). ⇒ **Relist chain is inferable only, never definitive.**
- **90-day retention** for cancelled/expired orders. Cache 3600 s. Rate limit
  null in spec.

---

## 5. Do market-listed items appear in `GET /characters/{id}/assets/`?

**No — items on an active sell order are held in market escrow and are not
returned by the assets route.** They only become visible again once the order
ends (fills, cancels, expires).

Primary-source basis:
- The assets response's `location_flag` enum in the current spec has **89 values
  and contains no market/escrow flag** (values are hangars, bays, slots,
  `Deliveries`, `AssetSafety`, etc.). An item held by the market has no
  `location_flag` that could represent it.
- `esi/eve-glue`'s `location_flag.py` likewise defines no market/escrow flag.
- **Labelled: strong structural inference, not an explicit CCP sentence.** No
  ESI documentation page, and no issue thread found, states the exclusion in
  words. The conclusion follows from the absence of any escrow flag plus the
  documented meaning of the orders route (`volume_remain` is the only
  representation of listed stock).

Practical implication: **inventory-on-market cannot be read from `/assets`; it
must be tracked via `/orders` + `/orders/history` (`volume_remain`) and the
`market_transaction`/`market_escrow` journal entries.** Free (non-escrowed)
stock is in `/assets`.

---

## 6. Detecting an on-behalf-of transfer (items leaving via contract/trade)

### Item-exchange contracts — YES, detectable

`GET /characters/{id}/contracts` — description: *"Returns contracts available
to a character, only if the character is issuer, acceptor or assignee. Only
returns contracts no older than 30 days, or if the status is `in_progress`."*
Relevant fields:
- `type` enum includes **`item_exchange`** (also `auction`, `courier`, `loan`,
  `unknown`).
- `status` enum: `outstanding`, `in_progress`, `finished_issuer`,
  `finished_contractor`, `finished`, `cancelled`, `rejected`, `failed`,
  `deleted`, `reversed`.
- `issuer_id`, `issuer_corporation_id`, `assignee_id`, `acceptor_id`, `price`
  (*"Price of contract (for ItemsExchange and Auctions)"*), `date_issued`,
  `date_accepted`, `date_completed`, `for_corporation`, `availability`,
  `start_location_id`/`end_location_id` (couriers), `title`.

`GET /characters/{id}/contracts/{contract_id}/items` (same scope) — fields
`record_id`, `type_id`, `quantity`, `is_singleton`, `is_included`
(*"true if the contract issuer has submitted this item … false if the issuer is
asking for this item"*), `raw_quantity` (*"-1 indicates … singleton … -1
Original and -2 Blueprint Copy"*). **This is the definitive record of which
`type_id`/`quantity` left (or entered) the character.**

Wallet side:
- If the item-exchange contract has `price > 0`, the character receives/spends
  ISK recorded in the journal as `contract_price` (personal) /
  `contract_price_payment_corp` (corp), with `context_id_type = contract_id`
  (eve-glue maps contract refs 63–84 → `contract_id`).
- If `price == 0` (a pure on-behalf-of/gift handoff), **there is no ISK
  movement and the wallet journal will not reveal the transfer at all.** Only
  the contracts routes expose it.
- Historical caveat: `esi/esi-issues#672` documented that contract wallet
  entries omitted `first_party_id`/`second_party_id`; the v4 journal added
  `description` + `context_id` and was released to fix party attribution. The
  contract `id` is still the reliable join key via `context_id`.

Retention: contracts list only 30 days (or `in_progress`) — **shorter than the
90-day order history**. Cache 300 s (5 min). Rate limit group `char-contract`,
`600 / 15m`.

### Direct player trade — NO, not detectable

There is **no ESI endpoint for the in-game trade window** (no
`/characters/{id}/trades` or similar anywhere in the spec path list). The wallet
journal only records the ISK legs (`player_trading`, `player_donation`) and does
not record item movement. ⇒ **Items handed over via direct trade are invisible
to ESI.** This is a hard limitation, not an uncertainty.

---

## 7. Quirks: cache/Expires, rate limits, near-real-time?

### Cache / Expires

- Wallet transactions & journal: `x-cache-age` / `x-server-cache-ttl` =
  **3600 s**. Open orders: **1200 s**. Order history: **3600 s**.
  Contracts + contract items: **300 s**. Assets: **3600 s**.
- Caching is currently **time-based TTL** (`x-server-cache-mode: ttl-based`).
  `esi-docs/docs/services/esi/best-practices.md`: the `Expires` header is when
  ESI's cache should expire; do **not** request before it, and circumventing the
  cache can get you **banned**. `ETag` + `If-None-Match` yields `304`.
- The dev blog *"Smarter caching: when events drive invalidation"*
  (2026-01-27) announces a move to **event-based invalidation**; `Expires`
  becomes meaningless (kept for back-compat) and `Cache-Control` becomes the
  signal. As of that post it was live **only** for `skills` and `skillqueue`;
  the wallet/orders/contracts routes were still time-based.
- **Near-real-time? No.** Wallet data can be up to **1 hour stale**; contracts up
  to 5 minutes. Poll at or after cache expiry.

### Rate limits

From the spec `x-rate-limit` + `esi-docs/docs/services/esi/rate-limiting.md`:

| Route group | Limit |
|---|---|
| `char-wallet` (transactions + journal) | 150 tokens / 15 min |
| `char-contract` (contracts + items) | 600 tokens / 15 min |
| `char-asset` | 1800 tokens / 15 min |
| character `orders` / `orders/history` | **null** in spec (not yet bucketed) |

Costs: 2XX = 2 tokens, 3XX = 1, 4XX = 5, 5XX = 0. Bucket is per
`applicationID:characterID`. A **separate global error limit** still applies:
at most **100 non-2xx/3xx per minute** across ESI, else `420`.
Separately, the `/markets/{region_id}/orders` route got a dedicated market-order
bucket of **12,000 tokens / 15 min** on 2026-02-24 (dev blog) — this is the
*public* market route, not the character routes, but it matters if you fetch
public order books for pricing.

### `from_id` quirks

- `from_id` on transactions is **inclusive** (returns the referenced record
  too), contradicting the spec wording "before". Tracked in
  `esi/esi-issues#1301` (open), `#715`, `#553`. Use the documented stop
  condition (stop when only the `from_id` record returns), not an equality check.
- Journal uses `page`/`X-Pages`, not `from_id` (older docs/issues referencing a
  journal `from_id` are stale).

### Other

- `id` fields are int64; several ESI ids have historically approached int32
  limits (`esi/esi-issues#719` comment on `transaction_id` int64), so store them
  as 64-bit.
- `esi-access.read_lists.v1` and unrelated SKINR scopes appear in the scope list
  but are irrelevant here.

---

## Unresolved / dead ends (findings in their own right)

1. **No documented transaction retention window.** The current spec omits it;
   the deprecated swagger's `maxItems: 2500` is gone; CCP's `#731` promise of a
   v2 doc note is not reflected in the current spec. Test empirically before
   assuming >30-day backfill.
2. **`brokers_fee` / `transaction_tax` have no order linkage** (`#1369` open).
   Exact per-relist fee attribution is impossible from ESI today; any tool must
   label these as allocated/estimated.
3. **No escrow flag and no explicit CCP statement** confirming market-escrowed
   items are absent from `/assets`; the conclusion is structural inference from
   the 89-value `location_flag` enum and eve-glue.
4. **Direct player trades are entirely invisible** (no endpoint, no item-side
   journal entry).
5. `esi/esi-issues#1456` (open): `market_escrow` journal `description` text
   differs from the in-game client — don't parse `description` for logic.
6. `esi/esi-issues#958` (closed 2025-07-16): historically bogus
   `market_escrow` `context_id`; believed resolved but not re-verified here.
