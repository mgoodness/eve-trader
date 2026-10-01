# `GET /characters/{character_id}/wallet/transactions/` as a cost-basis source — authoritative reference

Question: for eve-trader's trading-stock ledger, what exactly does
`GET /characters/{character_id}/wallet/transactions/` (scope
`esi-wallet.read_character_wallet.v1`) return, how does a transaction tie back
to a ledger entry, do broker fee and sales tax surface here or only in
`/wallet/journal/`, and does this route share `/wallet/journal/`'s cache/rate-limit
behaviour?

Written: **2026-10-01**. Live HTTP observations below were made on 2026-10-01
against `esi.evetech.net` (tranquility), unauthenticated (no character token was
available to this research task — every live check is a 401 probe, which is
enough to read rate-limit headers but not response bodies; this is called out
inline). Every claim is tagged **[stated]** (present verbatim in a primary
source — cited) or **[inferred]** (reasoning connecting stated facts). This note
assumes the reader has `docs/research/esi-sso-cli.md` (auth, compatibility-date,
rate-limit header mechanics) and `docs/research/eve-market-mechanics-and-esi.md`
(fee/tax formulas, the `char-wallet` rate-limit group, the wallet-journal
`ref_type`s) open already; it does not re-derive what those establish.

Bottom line for the ledger:

- **The transaction record carries no fee/tax fields at all** — `unit_price` is
  the order's own posted price (no slippage, confirmed by the fill mechanics in
  the sibling doc), and the record exists to confirm *that a fill happened* and
  to hand you a `journal_ref_id` pointer into the journal. **[stated]**
- **`/wallet/transactions/` alone cannot compute realised net margin.** The
  sale's ISK movement (`market_transaction`), the seller's sales tax
  (`transaction_tax`), and a buy fill's escrow release (`market_escrow`) are
  three *separate* journal rows, and the **broker fee is not linked to any
  transaction at all** — it is charged at order placement/modification, before
  any fill exists, and CCP's own tracker has an **8-year-old open feature
  request** asking for exactly this linkage. `/wallet/journal/` is required.
  **[stated]**
- **Transactions and journal share the same cache (3600 s) and the same
  `char-wallet` rate-limit group (150 tokens / 15 min)** — confirmed both from
  the live OpenAPI spec and from a live 401 response header on the transactions
  route itself. **[stated]**
- **Pagination is `from_id`-cursor, not `X-Pages`** — confirmed by both the
  OpenAPI parameter list and a current, dedicated CCP docs page ("From-id
  Pagination") that uses `transaction_id` as its own worked example. **[stated]**
- **The current spec dropped two limits that were in the 2021 Swagger**: a
  hard **`maxItems: 2500`** on the transaction array, and (for journal only) an
  explicit **"going 30 days back"** sentence. The live 2026 OpenAPI carries the
  30-day sentence for journal verbatim but states **no page-size or retention
  limit for transactions at all** — a silent regression, not a stated removal
  of the limit. Treat both historical limits as still probably true in practice
  and verify empirically before relying on either being gone. See
  [§6](#6-documentation-regressions-and-disagreements).

---

## Method / sources

| Source | URL / locator | Role |
|---|---|---|
| **ESI OpenAPI spec (live)** | <https://esi.evetech.net/meta/openapi.json>, fetched 2026-10-01 (`openapi 3.1.0`, `info.version 2020-01-01`) | `paths./characters/{character_id}/wallet/transactions`, `components.schemas.CharactersCharacterIdWalletTransactionsGet`, `components.schemas.CharactersCharacterIdWalletJournalGet`, rate-limit/cache `x-*` extensions |
| Live 401 probe, transactions route | `GET https://esi.evetech.net/latest/characters/123123/wallet/transactions/`, fetched 2026-10-01 | `x-ratelimit-group: char-wallet`, `x-ratelimit-limit: 150/15m` — confirms the group is shared, without needing a token |
| Live 401 probe, journal route | `GET https://esi.evetech.net/latest/characters/123123/wallet/journal/`, fetched 2026-10-01 | Same group/limit, for direct comparison |
| ESI docs — From-id Pagination | <https://developers.eveonline.com/docs/services/esi/pagination/from-id/> | Current, dedicated doc for the exact cursor model `/wallet/transactions/` uses; worked example literally named `transaction_id` |
| ESI docs — X-Pages Pagination | <https://developers.eveonline.com/docs/services/esi/pagination/x-pages/> | Confirms the *other* pagination style, used by journal but **not** by transactions |
| ESI docs — Best Practices | <https://developers.eveonline.com/docs/services/esi/best-practices/> | Error-limit vs bucket-limit header semantics (reused from sibling doc, not re-derived) |
| Archived ESI Swagger (Wayback, 2021-10-09) | `20211009225712id_/https://esi.evetech.net/latest/swagger.json` | `maxItems: 2500` on the transaction array; the pre-OpenAPI-3.1 shape of both routes |
| `esi/esi-glue` (CCP's own repo, GitHub org `esi`) | `eve_glue/wallet_journal_ref.py` | Canonical string↔int `ref_type` mapping (`brokers_fee=46`, `transaction_tax=54`, `market_transaction=2`, `market_escrow=42`); same CCP org as `esi/esi-issues` and `esi/esi-docs` cited in the sibling doc |
| `esi/esi-issues` issue #82 (CCP's ESI tracker) | <https://github.com/esi/esi-issues/issues/82>, opened 2016-11-13, **still open** | "Include order_id in response" — the authoritative statement that a transaction cannot be tied to the order that produced it except by inference |
| `esi/esi-issues` issue #1456 | <https://github.com/esi/esi-issues/issues/1456> | Live example journal entry showing `context_id`/`context_id_type: market_transaction_id` on a `market_escrow` row, dated 2025-12-27 |
| CCP Zoetrope (CCP staff), `esi/esi-issues` #650 | <https://github.com/esi/esi-issues/issues/650> | ref_type string↔int mapping is official, confirmed by CCP in the issue's resolution |
| EVE Online forums — *ESI market* (community thread, CCP-adjacent dev `Golden_Gnu` answering) | <https://forums.eveonline.com/t/esi-market/151176> | Community-stated (not CCP-authored) explanation that `context_id`/`context_id_type: market_transaction_id` links a journal row to a `transaction_id`; cited as corroborating, not primary |
| EVE Online forums — *Extending ESI wallet journal broker fee entries* | <https://forums.eveonline.com/t/extending-esi-wallet-journal-broker-fee-entries/225401> | The 2020 request (closed, moved to GitHub as #82) that broker-fee journal rows carry no order/item linkage |
| EVE Online forums — *Wallet transaction route limited to 30 days?* | <https://forums.eveonline.com/t/wallet-transaction-route-limited-to-30-days/488424> | Anecdotal (non-CCP) reports that the practical retention is ~30 days even though the current spec states no limit for transactions; flagged as unverified |
| Sibling doc | `docs/research/eve-market-mechanics-and-esi.md` §§1–2, 6.5, 6.6 | Fee/tax formulas, `char-wallet` group baseline (wallet + journal), caching discipline — not re-derived here |
| Sibling doc | `docs/research/esi-sso-cli.md` §4.1, §6.12–6.13 | `X-Compatibility-Date`, rate-limit header set, error-limit mechanics — not re-derived here |

CCP sources (the live OpenAPI spec, the `esi` GitHub org, and `developers.eveonline.com/docs`) win over forum/community statements wherever they disagree; forum posts are cited only to corroborate behaviour the primary sources state ambiguously or not at all, and are always marked as such.

---

## 1. Response shape

From `components.schemas.CharactersCharacterIdWalletTransactionsGet` in the live
OpenAPI spec, fetched 2026-10-01: **[stated]**

```json
{
  "description": "Wallet transactions",
  "type": "array",
  "items": {
    "description": "wallet transaction",
    "type": "object",
    "required": ["transaction_id", "date", "location_id", "type_id",
                 "unit_price", "quantity", "client_id", "is_buy",
                 "is_personal", "journal_ref_id"],
    "properties": {
      "transaction_id": {"type": "integer", "format": "int64",
        "description": "Unique transaction ID"},
      "date": {"type": "string", "format": "date-time",
        "description": "Date and time of transaction"},
      "location_id": {"type": "integer", "format": "int64"},
      "type_id": {"type": "integer", "format": "int64"},
      "unit_price": {"type": "number", "format": "double",
        "description": "Amount paid per unit"},
      "quantity": {"type": "integer", "format": "int64"},
      "client_id": {"type": "integer", "format": "int64"},
      "is_buy": {"type": "boolean"},
      "is_personal": {"type": "boolean"},
      "journal_ref_id": {"type": "integer", "format": "int64"}
    }
  }
}
```

All nine properties are `required` — **every field is always present**, no
optional/nullable fields on this record. **[stated]**

| Field | Meaning | Notes |
|---|---|---|
| `transaction_id` | Unique ID of this fill | **[stated]** Primary key for this record; also what you pass back as `from_id` to page further into the past |
| `date` | Timestamp of the fill | **[stated]** ISO 8601 |
| `type_id` | The item sold/bought | **[stated]** |
| `unit_price` | "Amount paid per unit" | **[stated]** This is the order's own posted price — no slippage is possible in EVE's order-matching model (order book, not an AMM), so this field is not discovering an unknown market price, it is **confirming the specific posted price your order (or the order you matched) cleared at**. Combined with `quantity` this gives the **gross** trade value; it is *not* net of broker fee or sales tax |
| `quantity` | Units filled | **[stated]** |
| `client_id` | The counterparty (character or NPC corp id) to the trade | **[stated]** No description beyond the field name in the current schema (the 2021 archived swagger called it simply "client_id integer", equally uninformative) |
| `is_buy` | `true` if *this character* bought | **[stated]** |
| `is_personal` | Personal vs. corporation wallet | **[stated]** Present in the character endpoint's schema even though corp-vs-personal context doesn't obviously apply to `/characters/.../wallet/...` — not explained further by the spec; **not found** why this field exists on the character route at all |
| `journal_ref_id` | Pointer into the wallet journal | **[stated]** The `id` of the **`market_transaction`** journal row for this specific fill (see [§2](#2-matching-a-transaction-to-a-ledger-entry)) — this is the *only* cross-reference the record gives you |
| `location_id` | Where the trade happened | **[stated]** Station or structure ID |

No `price`, `fee`, `tax`, `broker_fee`, `net_price`, or `order_id` field exists
on this record. **[stated — by absence from the schema above]**

---

## 2. Matching a transaction to a ledger entry

### 2.1 What the transaction record gives you for free

Because EVE's market is an order book (not an AMM), **a transaction always
clears at the order's own posted price** — the engine never needs
`/wallet/transactions/` to *discover* a price it didn't already quote. What the
route is for:

- **Confirming a fill happened** (and when, and for how much quantity) — the
  engine's own order-tracking (via
  `GET /characters/{character_id}/orders/` + `/orders/history/`, documented in
  the sibling doc §6.5) can tell you volume_remain dropped, but only
  `/wallet/transactions/` gives you the ISK-movement-level confirmation with a
  `transaction_id` you can store as "this fill has been booked." **[inferred]**
- **Handing you `journal_ref_id`**, the one and only structured link from a
  transaction to the wallet journal, so you can pull the matching
  `market_transaction` row (amount, balance-after) without scanning the whole
  journal for a date/amount match. **[stated]**

### 2.2 `journal_ref_id` → journal, exactly

`journal_ref_id` is the `id` of the journal entry whose `ref_type` is
`market_transaction` (string form) / `2` (integer form, per the CCP-owned
`esi/eve-glue` mapping, `eve_glue/wallet_journal_ref.py`:
`market_transaction = 2`). **[stated]** This is the ISK-movement row for the
fill itself — a negative `amount` on the buy side, positive on the sell side —
and is **distinct** from the fee/tax rows discussed in [§3](#3-fee-and-tax-surfacing).

### 2.3 There is no `order_id` on a transaction — confirmed as a known, unresolved gap

**[stated]** CCP's own tracker has an **8+ year old, still-open** feature
request:

> "The transaction endpoint model is already showing a journal_ref_id,
> wondering if we could include an order_id as well to the transaction
> response. This will make linking transactions to orders a lot easier. Same
> goes for the corporation endpoint."
> — <https://github.com/esi/esi-issues/issues/82> (opened 2016-11-13 by
> `nic-southern`, state **open**, last updated 2020-03-09)

A later comment on that same issue states the exact use case this research
question is about:

> "+1 for this please. I require this for calculating the tax and broker fee to
> apply to each wallet transaction for profit tracking. At the moment the
> wallet transaction returns the station that the transaction took place and
> there is no link to where the actual order was placed. This means correct
> tax can not be calculated. Linking to the OrderId would allow us to work this
> out."
> — same issue, quoted verbatim

A separate 2020 forum thread asking for the broker-fee *journal* entry to carry
order/item context was closed and redirected to the same GitHub issue
(**[stated]**, <https://forums.eveonline.com/t/extending-esi-wallet-journal-broker-fee-entries/225401>).
**No fetched source shows this ever being resolved** — the live 2026 OpenAPI
schema (checked in [§1](#1-response-shape)) still has no `order_id` field on
either the transaction or the journal.

**[inferred — what the engine must do instead]** Match a transaction to *the
ledger entry you created when the order was placed* by `type_id` +
`location_id` + `unit_price` (equal to the order's posted price, per
[§2.1](#21-what-the-transaction-record-gives-you-for-free)) + `is_buy`, scoped
to the time window the order was live, and consuming `quantity` against the
ledger entry's remaining open quantity (partial fills are multiple transaction
rows against one ledger entry). This is necessarily a **heuristic match**, not
an ESI-guaranteed join — a community reply on a related thread states the same
fallback explicitly:

> "I would like to add that others have linked orders and transactions by
> comparing: `type_id`, `location_id`, `quantity`|`volume_total`|`volume_remain`,
> `unit_price`|`price`… you may be able to improve the comparison even more…
> But, in short, it's a fuzzy comparison that can have collisions."
> — <https://forums.eveonline.com/t/esi-market/151176> (community reply, not
> CCP; cited as corroborating the lack of a hard join, not as a technique
> endorsed by CCP)

Because the engine already knows the order's posted price (it set it), this
fuzzy match only needs to disambiguate *which* open ledger entry a fill
belongs to when several are open on the same `type_id` at the same
`location_id` — not recover an unknown price.

---

## 3. Fee and tax surfacing — and why `/wallet/journal/` is required

### 3.1 The sibling doc's three `ref_type`s, confirmed present in the live enum

The live OpenAPI `CharactersCharacterIdWalletJournalGet.items.properties.ref_type.enum`
(162 entries, checked 2026-10-01) contains all three ref_types the sibling doc
names, confirming them against the current spec rather than only the 2021
archive: **[stated]**

| `ref_type` | Present in live enum? | CCP `esi/eve-glue` integer (`wallet_journal_ref.py`) |
|---|---|---|
| `brokers_fee` | yes | `46` |
| `transaction_tax` | yes | `54` |
| `market_escrow` | yes | `42` |
| `market_transaction` | yes | `2` |

The CCP-staff resolution of `esi/esi-issues` #650 confirms this string↔int
mapping is official and CCP-maintained ("we can make the string->int mapping
public… I put the mapping in this gist", linked to the file that became
`esi/eve-glue`'s `wallet_journal_ref.py`). **[stated]**

### 3.2 A journal entry now carries its own `tax`/`tax_receiver_id` fields — new since the 2021 archive

The live schema has two fields **absent from the archived 2021 Swagger**:

```json
"tax": {"type": "number", "format": "double",
  "description": "Tax amount received. Only applies to tax related transactions"},
"tax_receiver_id": {"type": "integer", "format": "int64",
  "description": "The corporation ID receiving any tax paid. Only applies to tax related transactions"}
```

— `components.schemas.CharactersCharacterIdWalletJournalGet.items.properties`,
live OpenAPI spec. **[stated]** Confirmed by diff against the archived 2021
Swagger's journal item schema, which has `amount, balance, context_id,
context_id_type, date, description, first_party_id, id, reason, ref_type,
second_party_id` and **no** `tax`/`tax_receiver_id`. **[stated]**

Neither field's description states *which* `ref_type`s populate it beyond "tax
related transactions" — **not found**: no fetched primary source enumerates the
exact ref_type set that sets `tax`/`tax_receiver_id`. **[inferred]** `transaction_tax`
is the obvious candidate (and plausibly `contract_sales_tax`,
`cosmetic_market_skin_sale_tax`, etc., given the "receiver" framing matches
"always paid to NPC corporation SCC" from the sibling doc §2); verify against a
live `transaction_tax` row before depending on it, since it is unverified here
(no authenticated fetch was available to this task).

### 3.3 `context_id`/`context_id_type` links a *fill-time* journal row back to the transaction — but not the broker fee

The journal schema's `context_id_type` enum includes `market_transaction_id`
(live OpenAPI, confirmed in [§1 of the sibling doc's §6.5 table context]):
**[stated]**

```json
"context_id_type": {"enum": ["structure_id", "station_id",
  "market_transaction_id", "character_id", "corporation_id", "alliance_id",
  "eve_system", "industry_job_id", "contract_id", "planet_id", "system_id",
  "type_id"]}
```

A live example from CCP's own tracker shows this in practice on a
`market_escrow` row:

```json
{
  "amount": -16007970, "balance": 1917834436.0065,
  "context_id": 6699170020, "context_id_type": "market_transaction_id",
  "date": "2025-12-27T16:40:08Z", "ref_type": "market_escrow",
  "description": "Market escrow release", ...
}
```
— <https://github.com/esi/esi-issues/issues/1456> (bug report about the
`description` text, filed against a real response body; the `context_id`/
`context_id_type` pair is incidental to that bug but is exactly the join key in
question). **[stated — observed response, not an abstract spec claim]**

A community (non-CCP) reply corroborates the general rule:

> "The journal have `context_id` and `context_id_type` (`market_transaction_id`)
> to link it to a transaction_id, however, it does not have an order_id."
> — <https://forums.eveonline.com/t/esi-market/151176>, community reply.
> **[corroborating, not primary]**

**[inferred]** Putting §3.1–§3.3 together: a `transaction_tax` row and a
`market_escrow` row, both being *consequences of a fill*, are expected to carry
`context_id_type: market_transaction_id` with `context_id` equal to the
`transaction_id` from `/wallet/transactions/` — **but this was only directly
observed for `market_escrow`** in the fetched sources; no fetched example shows
a `transaction_tax` row's `context_id_type`. Treat the `transaction_tax` case as
**plausible but unverified** until checked against a live character's journal.

**`brokers_fee` is structurally different and does *not* get this linkage.**
The broker fee is charged when an order is **placed or modified**
(sibling doc §1), which is *before* any transaction exists — there is no
`transaction_id` for a `brokers_fee` row to reference. This is exactly the
long-standing complaint in `esi/esi-issues` #82 and the 2020 forum thread
(§2.3): CCP has never added an order/transaction linkage to `brokers_fee`
journal rows, and the live spec's `context_id_type` enum has no
`order_id`-shaped member at all (the closest is `station_id`/`structure_id`,
i.e. *where* the order was placed, not *which* order). **[stated, by absence]**

### 3.4 Answer: `/wallet/transactions/` alone does **not** suffice for realised net margin

Putting the above together, computing a sale's realised net margin requires:

| Quantity | Where it lives |
|---|---|
| Gross sale proceeds (`unit_price × quantity`) | `/wallet/transactions/` (confirmed fill) or the `market_transaction` journal row via `journal_ref_id` |
| Sales tax actually deducted | `/wallet/journal/`, `ref_type: transaction_tax` row — **not present anywhere in the transaction record** |
| Broker fee paid to list the sell order | `/wallet/journal/`, `ref_type: brokers_fee` row, **matched to the order by heuristic (station/time), not by any ESI-provided key** — also not present in the transaction record, and not even reliably linkable to *the transaction* the way `transaction_tax` plausibly is |
| Cost basis of the goods sold (the buy side) | The engine's own ledger (ESI has no concept of cost basis; this is domain logic, not an ESI field) |

**[inferred — the deliverable answer]** `/wallet/transactions/` is necessary
(it is the only route that confirms *a fill happened* with an ESI-assigned
`transaction_id` and hands you `journal_ref_id`) but **not sufficient**: both
fee components live exclusively in `/wallet/journal/`, and the sibling doc's
formula-based estimate (`broker_fee_% × order value`, `sales_tax_% ×
sell value`) can be computed **without** ESI at all if the engine already knows
the trader's skills/standings — but the task brief's own framing (confirming a
fill, not deriving price) extends naturally to **confirming the fee actually
charged**, which only the journal states as an ISK figure. A ledger that wants
audited (not merely estimated) net margin per sale must fetch both routes and
join on `journal_ref_id` (for the sale itself) plus a time/station heuristic
(for the broker fee, since no stronger key exists per §3.3).

---

## 4. Cache, ETag, pagination, and rate limits

### 4.1 Cache behaviour — identical declared TTL to journal

From the live OpenAPI spec, the `get` operation for
`/characters/{character_id}/wallet/transactions`: **[stated]**

```json
"x-cache-age": 3600,
"x-client-cache-ttl": 3600,
"x-server-cache-ttl": 3600,
"x-server-cache-mode": "ttl-based",
"x-rate-limit": {"group": "char-wallet", "max-tokens": 150, "window-size": "15m"}
```

This is **identical** to `/characters/{character_id}/wallet/journal`'s
declared cache/rate-limit block in the same spec (same four `x-*cache*` values,
same `x-rate-limit` object) — confirmed by direct comparison of both path
objects in `/tmp/openapi.json` fetched 2026-10-01. **[stated]**

Response headers declared for a 200: `Cache-Control`, `ETag`, `Last-Modified`
(the generic `components.headers` definitions — "Use this with If-None-Match…"
/ "Use this with If-Modified-Since…"). **[stated]** No `Expires` header is
declared on either route in the current spec (the archived 2021 Swagger did
declare `Expires`; this is consistent with every other route checked for the
sibling doc, e.g. `/markets/{region_id}/orders` also dropped `Expires` for
`Cache-Control`/`ETag`/`Last-Modified` — a spec-wide OpenAPI-3.1 migration
artefact, not something specific to wallet routes). **[stated]**

### 4.2 Live confirmation: transactions and journal share the `char-wallet` bucket

Both routes were probed unauthenticated (401, since no character token was
available) on 2026-10-01 and both returned identical rate-limit headers:

```
GET /latest/characters/123123/wallet/transactions/  → 401
  x-ratelimit-group: char-wallet
  x-ratelimit-limit: 150/15m
GET /latest/characters/123123/wallet/journal/  → 401
  x-ratelimit-group: char-wallet
  x-ratelimit-limit: 150/15m
```

**[stated — live observation]** This confirms directly, without needing a
valid token, that transactions shares the exact `char-wallet` group the sibling
doc documents for wallet/journal (and for `GET /characters/{character_id}/wallet/`
itself) — **150 tokens / 15 min total across all three routes**, not 150 each.
**[inferred from the shared group name]** A ledger that polls both
`/wallet/transactions/` and `/wallet/journal/` on the same cadence is spending
from one shared 150-token/15-min budget, not two independent ones; at 2XX = 2
tokens/request (sibling doc §6.6), that is at most 75 combined successful
requests across all three wallet routes per 15-minute window before
rate-limiting.

A 401 does not confirm the `Cache-Control`/`ETag` response headers (those are
only declared on the 200 response and a real auth error short-circuits before
the cache layer runs); this was **not verified live** in this task for lack of
a token. **[stated limitation of this research]**

### 4.3 Pagination: `from_id` cursor, not `X-Pages` — and this is current, documented CCP behaviour

The transactions route's only pagination parameter is `from_id` (query,
`int64`, "Only show transactions happened before the one referenced by this
id"); there is **no** `page` parameter and **no** `X-Pages` response header
declared for this route in the live spec. **[stated]** This is the opposite
shape from `/wallet/journal/`, which **does** declare `page` and an `X-Pages`
response header (confirmed side-by-side in [§1](#1-response-shape)'s path
objects).

CCP's current developer docs describe exactly this `from_id` model as its own
named pagination style, with a worked example that literally uses the field
name `transaction_id`:

> "From-id pagination uses record IDs to navigate backwards through datasets in
> chronological order… Use the `transaction_id` of the last record as `from_id`
> in your next request. The response will always include that `from_id`
> record… If it only contains that one record, stop."
> — <https://developers.eveonline.com/docs/services/esi/pagination/from-id/>,
> fetched 2026-10-01 **[stated]**

This doc page states **no maximum record count per request** and **no day/age
retention limit** for from_id-paginated routes in general. **[stated — by
silence]** Contrast with the separate "X-Pages Pagination" doc
(<https://developers.eveonline.com/docs/services/esi/pagination/x-pages/>),
which `/wallet/journal/` uses instead — two different, both currently
documented, pagination models on two routes that otherwise share every other
characteristic (scope, cache TTL, rate-limit group). **[stated]**

---

## 5. Live retrieval checklist (for the engine)

**[inferred, synthesising §§1–4]**

1. Call `/wallet/transactions/` with no `from_id` to get the most recent fills;
   page backwards with `from_id = <oldest transaction_id so far>` until a
   response contains only the `from_id` record itself (§4.3), or an already-seen
   `transaction_id` appears (cheaper stop condition if the ledger has prior
   state).
2. For each new `transaction_id`, record `journal_ref_id` and treat the
   transaction as "confirmed, price known" immediately — no need to resolve
   price from anywhere else (§2.1).
3. Separately page `/wallet/journal/` (its own `page`/`X-Pages` cursor, §4.3)
   and index rows by `ref_type`. Pull the `market_transaction` row matching
   each `journal_ref_id` if you want the exact `amount`/`balance` snapshot, and
   independently pull `transaction_tax` and `brokers_fee` rows for the same
   period to compute net margin (§3.4).
4. Join `transaction_tax` rows to transactions via `context_id_type ==
   "market_transaction_id"` and `context_id == transaction_id` **once this has
   been empirically verified** against a real character (§3.3 flags this as
   unverified here).
5. Join `brokers_fee` rows to the engine's own ledger entries by
   station/structure (`location_id`) and time proximity to the order's
   `issued` timestamp — there is no stronger key (§3.3, §2.3).
6. Both calls draw from the same 150-token/15-min `char-wallet` bucket shared
   with `/wallet/` itself (§4.2); budget accordingly if polling all three.

---

## 6. Documentation regressions and disagreements

1. **`maxItems: 2500` vanished from the current spec.** The archived 2021
   Swagger states `"maxItems": 2500` on the transaction array's schema (and
   the same figure independently on the journal array, per a third-party
   OpenAPI mirror of the 2021-era corp-wallet-journal schema found during this
   research). The live 2026 OpenAPI has **no `maxItems` anywhere in either
   schema**. **No fetched CCP source states the limit was lifted**; this
   matches the exact pattern the sibling doc documents for corporation-role
   annotations disappearing from the spec (sibling doc §5, "Confirmed
   documentation regression"). **Winner: treat the 2,500-row cap as still
   probably true in practice** (nothing contradicts it) but unenforced-by-spec,
   and verify empirically — do not write code that assumes either "always
   ≤2500" or "no cap" without checking a real response.
2. **Journal states a 30-day retention window; transactions states none.** The
   live OpenAPI description for `/wallet/journal/` is verbatim "Retrieve the
   given character's wallet journal **going 30 days back**"; the description
   for `/wallet/transactions/` is only "Get wallet transactions of a
   character" — no window stated at all, in either the 2021 archive or the
   live spec. A 2025 forum thread reports users **empirically** hitting a
   ~30-day practical limit on transactions too, with a community reply ("Some
   of the endpoints have a limit of X days or 10000 entries. That is just how
   ESI works.") that is **not a CCP statement** and gives no citation.
   **Winner: no primary source states a transactions-specific retention
   window; treat the 30-day figure as plausible-but-unconfirmed for
   transactions specifically**, and do not rely on being able to pull a trading
   history older than ~30 days from this route for cost-basis reconstruction —
   the engine's own ledger, not ESI, must be the system of record for anything
   older.
3. **`tax`/`tax_receiver_id` are new, undocumented-in-detail fields.** They
   exist in the live schema (§3.2) but their description does not enumerate
   which `ref_type`s populate them. **Not found**: no changelog entry, blog
   post, or `esi-issues` thread fetched during this research explains when
   these fields were added or which ref_types set them. Flagged as an open
   question below rather than guessed.

---

## Open questions / not found

- **Which `ref_type`s populate the journal's `tax`/`tax_receiver_id` fields?**
  Not stated anywhere fetched beyond "tax related transactions." Verify against
  a live `transaction_tax` row before using these fields as an authoritative
  fee source instead of `amount`.
- **Does a `transaction_tax` journal row actually carry
  `context_id_type: market_transaction_id`?** Only a `market_escrow` row was
  observed with that pairing (`esi-issues` #1456); no fetched example shows a
  `transaction_tax` row's `context_id`/`context_id_type`. The community
  forum claim that "the journal" generally does this is stated without a
  `transaction_tax`-specific example.
- **Is `/wallet/transactions/` actually capped at 2,500 rows per call, or at
  some other number, today?** The number the current spec no longer states is
  the number everyone still quotes; this research found no live authenticated
  response to confirm or refute it (no character token was available to this
  task).
- **Does `/wallet/transactions/` actually enforce a ~30-day retention window
  like `/wallet/journal/` states for itself?** Anecdotal forum reports say yes;
  no primary CCP source states a number for this specific route.
- **`is_personal`'s purpose on the *character* wallet route.** The field exists
  and is `required`, but no fetched source explains why a single-wallet
  character endpoint needs a personal-vs-not flag; presumably an artefact
  shared with the corporation wallet schema's handling of personal vs.
  divisional transactions, but this is inferred, not stated.
- **Exact live response headers (`Cache-Control`, `ETag` value shape, 304
  behaviour) for a real 200 on this route.** Not verified in this task — no
  character access token was available; only unauthenticated 401 probes were
  possible, which surface rate-limit headers but not the cache headers that
  only appear on a successful response.
