# eve-trader v1.1 — specification

Status: **draft for review**. v1 assembled from the closed decisions on the
[v1 wayfinder map](https://github.com/mgoodness/eve-trader/issues/1); v1.1 adds
inventory awareness, assembled from the closed decisions on the
[inventory-aware recommendations wayfinder map](https://github.com/mgoodness/eve-trader/issues/33).
Every section cites the ticket that settled it; the locked mechanics are
cross-checked against `docs/research/`.

---

## 1. Summary

`eve-trader` is an on-demand command-line tool. It reads live market data for the
**Heimatar** region over ESI, plus the pilot's character skills, standings, and
**held trading stock**, and recommends — per candidate item — a **buy
recommendation** (price and quantity to post at the front of the **Rens** order
book) and, independently, a **sell recommendation** (price and quantity for stock
it already holds) — each clearing a target net margin where applicable, under a
stated budget and order limit.

It recommends; it never places orders. A buy recommendation never assumes a
matching sell order follows immediately — posting a sell order needs stock a buy
order hasn't necessarily delivered yet, which is what v1.1 adds the machinery to
track.

## 2. Scope

**In scope.** One trade station (Rens VI − Moon 8 − Brutor Tribe Treasury,
`60004588`, in Rens system `30002510`, Heimatar `10000030`); NPC stations only;
one character; a single on-demand run producing a ranked recommendation set;
**(v1.1)** a local ledger tracking this pilot's own trading stock, so sell
recommendations only ever cover stock actually held and not already listed.

**Out of scope** (see the v1 map's and the inventory map's *Out of scope*).
Hauling and regional arbitrage; the pilot's own regional-range buy orders;
Upwell/structure markets; other regions or hubs; web/TUI/daemon/MCP surfaces; a
fill-probability model, an aggression knob for δ, and relist/undercut alerts; the
ex-post validation harness and capture calibration; trading on behalf of other
characters as a distinct, separately-budgeted feature. All are v2 or a different
product.

## 3. Domain model

The vocabulary is `CONTEXT.md`; it is normative for this spec. The load-bearing
distinction is the two books:

- **Effective buy book** — every buy order whose range covers the trade station,
  wherever it sits in Heimatar. These are the competing bids, and also the prices
  at which held stock can be sold immediately.
- **Effective sell book** — the sell orders located at the trade station. Sellers
  elsewhere do not compete for the trade station's buyers.

**(v1.1)** A second load-bearing distinction: the **ledger** versus raw ESI
assets. The pilot's hangar at the trade station holds trading stock *and*
unrelated clutter (personal items, stock bought for other characters) — live-
confirmed only 4 of 40 asset rows at Rens were actually trading-relevant. The
ledger, not raw assets, is the sell-side source of truth; see §7.

## 4. Locked mechanics

Facts, not choices; sources in `docs/research/eve-market-mechanics-and-esi.md`,
`docs/research/esi-assets-and-orders.md`, `docs/research/esi-wallet-transactions.md`.

| Thing | Value |
|---|---|
| Broker fee (NPC) | `max(1%, 3% − 0.3%·BrokerRelations − 0.03%·factionStanding − 0.02%·corpStanding)`, charged on **each** order leg at creation, minimum 100 ISK per order |
| Sales tax | `7.5% × (1 − 0.11·Accounting)`, seller side only, deducted at sale |
| Buy-order escrow | 100% of order value, locked at creation (no Margin Trading) |
| Order limit | `5 + 4·Trade + 8·Retail + 16·Wholesale + 32·Tycoon` |
| Range enum | `station`, `solarsystem`, `region`, or `1,2,3,4,5,10,20,30,40` jumps |
| **(v1.1)** `/orders/history/` `state` enum | `cancelled`, `expired` **only — no `filled` value exists** |
| **(v1.1)** Character hangar | exactly **one** `Hangar` location per station; multi-division hangars are corporation-only |

**The pilot** (from [Provision the ESI application and CLI token flow](https://github.com/mgoodness/eve-trader/issues/8),
re-verified for the expanded scopes by [Provision expanded ESI scopes and
re-authorize the CLI token](https://github.com/mgoodness/eve-trader/issues/36)):
character `2119373348` (Aller Gaterau), **Trade 4, Broker Relations 4, Accounting 3**
→ **21 active orders**, **broker 1.8%**, **sales tax 5.025%**. The engine reads
these at runtime; it must not assume max skills.

## 5. Architecture

A **pure engine package** (no I/O, no CLI concerns) and a **thin CLI adapter**.
The engine's public surface is the JSON result (§14); every future surface — web,
TUI, daemon, MCP — is another adapter over the same engine. **(v1.1)** The engine
also owns the ledger (§7) as part of its state, reconciled once per invocation; no
web code in v1.1.

## 6. Data pipeline

Settled by [Decide the ESI data sync](https://github.com/mgoodness/eve-trader/issues/3)
and [Decide the buy-order range coverage model](https://github.com/mgoodness/eve-trader/issues/10).

**Ingest.** Pull the **whole Heimatar region-orders feed**
(`GET /markets/10000030/orders/?order_type=all`, ~71 pages, 300 s cache) and derive
the universe from it. `/markets/{region}/types/` is **not** used — it is paginated
(~11,000 types) and duplicates the feed.

**Books.** Build the effective books (§3):

- sell side: sell orders located at `60004588`;
- buy side: buy orders whose range covers `60004588`, evaluated **exactly** —
  `station`/`solarsystem`/`region` by match, numeric by
  `jumpDistance(system, 30002510) ≤ range`;
- **NPC-station locations only**; structure-located orders are excluded (~20% of
  buys).

**Jump distances.** From `GET /route/{system}/30002510/` (cached 86,400 s), computed
once per run for systems not yet cached. A failed lookup means the order does
**not** cover the station, and warns.

**History funnel.** History is one call per type. Apply the **book-only filters
first**, then fetch history only for survivors:

| Book-only filter | Types passing (of 3,972 two-sided) |
|---|---|
| spread > 2δ | 3,747 |
| margin ≥ target | 3,905 |
| non-thin book | 913 |
| **all three together** | **826** |

The history-dependent filters (§8: min history, min ADV, price band) then run on
those 826. History is cached until 11:05 daily, so this is one pass per day.

**Caching & politeness.** A local disk cache keyed by URL with each route's ESI
TTL: orders **300 s**, history **until 11:05**, jumps **86,400 s**, skills **60 s**,
standings **3,600 s**. Use `If-None-Match`/ETag; refetch *all* pages of a paginated
resource together when page 1 nears expiry; back off on 429/420.

## 7. Inventory ledger

**(v1.1)** Settled by [Design the trading-stock ledger](https://github.com/mgoodness/eve-trader/issues/37),
[Decide ledger bootstrap for pre-existing holdings](https://github.com/mgoodness/eve-trader/issues/38),
researched by [Research: ESI assets and character-order endpoints for ledger
reconciliation](https://github.com/mgoodness/eve-trader/issues/34). Design
rationale: [ADR 0003](adr/0003-json-lot-ledger-for-trading-stock.md),
[ADR 0004](adr/0004-explicit-seeding-for-pre-existing-holdings.md).

**Schema.** One **lot** per buy order that has delivered or is delivering stock —
acquisition price only ever varies *across* lots of the same type, never within
one, since a buy order fills at its own posted price with no slippage:

```
Lot {
  lot_id, type_id, source_order_id (or "seeded"),
  quantity_total, quantity_available,
  acquisition_price (null for a seeded lot),
  acquired_at,
  status: open-buy | held-unlisted | reserved-for-sale | sold
}
```

A sell order **reserves** quantity from the oldest available lot(s) (FIFO);
cancelling it releases the reservation; filling it finalizes to `sold`. Sold lots
are kept, not deleted.

**Storage.** A flat JSON file, `~/.local/state/eve-trader/ledger.json`,
atomic-write (temp file + rename, matching `credentials.json`'s pattern). The
skill-derived order limit (21–30 active orders) bounds the ledger at a few dozen
rows — too small to justify an embedded database.

**Reconciliation.** Once per run, at the start:

1. Poll `GET /characters/{id}/orders/`. For every lot still `open-buy`, a
   `volume_remain` decrease since last-seen is an authoritative partial fill.
2. When a lot's order disappears from `/orders/`, check
   `GET /characters/{id}/orders/history/` for that `order_id`. Found with
   `state: cancelled`/`expired` → authoritative terminal state, the last-seen
   `volume_remain` never arrives. Not found there (`/orders/history/` has no
   `filled` state — §4) and last-seen `volume_remain: 0` → confirmed full fill.
   Not found and `volume_remain > 0` → **unknown outcome**, surfaced to the
   pilot (§11 `Pending`), never guessed at.
3. Poll `GET /characters/{id}/assets/`, filtered to `location_flag: "Hangar"` at
   `60004588`, as a **ceiling-only check**: if assets show *less* than the
   ledger's held-unlisted quantity for a lot, clamp the ledger down with a
   visible warning (drift). Assets showing *more* is untracked clutter and is
   never pulled in.

**Bootstrap.** Pre-existing hangar stock the tool never bought is invisible to
the ledger by construction. An explicit, pilot-driven action lists untracked
`Hangar` stock and lets the pilot confirm each item is trading stock, creating a
**seeded lot** directly at `held-unlisted` (skipping `open-buy`) with
`acquisition_price: null`. Seeding is **never automatic** — auto-absorbing the
hangar would readmit the clutter the ledger exists to exclude.

## 8. Filter layer

Settled by [Decide candidate filtering, ranking, and volume capture](https://github.com/mgoodness/eve-trader/issues/4).
Applies to **buy recommendations only** (§11) — sell recommendations bypass it
entirely (§7, §11). Hard exclusions, applied in order, each with a recorded
reason (`--explain`):

1. **Universe** — only types with a live Heimatar order.
2. **Two-sided book** — a covering best bid *and* a station best ask.
3. **Min history** — ≥ 7 days of 30-day history.
4. **Min liquidity** — 30-day ADV ≥ 20 units/day.
5. **Gross-margin ceiling** — drop gross margin > 80%.
6. **Thin book** — require ≥ 2 orders within 5% of best on each side.
7. **Price band** — drop if best bid < 0.75× or best ask > 1.25× the 30-day low/high.
8. **Pricing rule** (§9) — drop if `2δ ≥ spread` or margin < target.

**Execution order.** The book-only filters — 2 (two-sided), 5 (gross ceiling),
6 (thin book), and the pricing rule 8 — run before any history call; the
history-dependent filters — 3 (min history), 4 (min ADV), 7 (price band) — run
after, on the survivors.

## 9. Pricing rule

Settled by [Prototype the front-of-queue pricing search on live Rens data](https://github.com/mgoodness/eve-trader/issues/2).
Governs **buy recommendations**; a sell recommendation's margin uses a different
`B` term entirely (§12).

```
B* = best bid + δ
S* = best ask − δ
net margin = (S* − B* − broker·B* − broker·S* − tax·S*) / S*
```

with `broker` and `tax` at the pilot's skills and standings. Recommend the item
**only if** `net margin ≥ target`. The target is a **filter** on the
front-of-queue prices for a buy recommendation; prices are never backed off to
manufacture it. **δ is a configurable step, default 100 ISK** (not 0.01).

Edge cases the implementation must handle, all reproduced in the prototype:

- `2δ ≥ spread` → `B* ≥ S*` → no recommendation (at δ = 100, an item needs a
  spread > 200 ISK);
- fees dominate thin spreads — at the pilot's real rates the round-trip cost is
  8.625%, so the ask must clear well above the bid;
- the 100 ISK minimum broker fee per order, modelled at order sizing;
- thin / manipulated books (the filter layer removes them).

## 10. Ranking

Settled by [Decide candidate filtering, ranking, and volume capture](https://github.com/mgoodness/eve-trader/issues/4).
Governs **buy recommendations only**. Candidates are ranked by **expected daily
profit**:

```
EDP = net profit per unit × capture rate × 30-day ADV
```

with a **flat capture rate of 20% of 30-day ADV** (configurable). EDP is a daily
*rate*, independent of how many units are posted. The row also exposes net margin,
ROI on escrow, and EDP per order slot. The 20% is the model's largest assumption;
calibration is v2 (§18).

**(v1.1)** Sell recommendations are not ranked by EDP — nothing is being rationed
(§12). They sort by worst margin shortfall first (§11).

## 11. Recommendation lifecycle

**(v1.1)** Settled by [Redesign the Recommendation model for the buy/sell
lifecycle](https://github.com/mgoodness/eve-trader/issues/39). Design rationale:
[ADR 0005](adr/0005-split-recommendation-into-buy-and-sell.md).

`Recommendation` (`CONTEXT.md`) is either a **buy recommendation** or a **sell
recommendation** — the two no longer imply each other and have genuinely disjoint
pipelines. A type_id can independently appear in the buy list, the sell list,
both, or neither in the same run.

- **Buy recommendation** — unchanged from v1 (§8–10): filter layer → pricing rule
  → EDP ranking → allocation (§13) → funded/unfunded/excluded.
- **Sell recommendation** — one per type_id with held-unlisted ledger stock,
  always covering the **full** available quantity (never partial — stock already
  paid for is never worth holding back). Bypasses the filter layer and EDP
  ranking entirely. Flagged, not excluded, when its margin (§12) falls under
  target. Sorted by worst margin shortfall first.
- **Pending** — one bucket, `reason`-tagged, for everything in flight and
  non-actionable this run: `awaiting-buy-fill` (an `open-buy` lot), `awaiting-
  sell-fill` (a `reserved-for-sale` lot — not re-recommended), `unknown-outcome`
  (§7), or `order-limit-exhausted` (§13). Mirrors the `excluded` bucket's
  count-by-default/`--explain`-detail pattern. Never counted toward funded,
  unfunded, excluded, or sell recommendations.

## 12. Margin-gate for held stock

**(v1.1)** Settled by [Decide margin-gate and cost-basis display for held
stock](https://github.com/mgoodness/eve-trader/issues/40), researched by
[Research: ESI wallet-transaction endpoint for cost-basis on held
stock](https://github.com/mgoodness/eve-trader/issues/35). Design rationale:
[ADR 0006](adr/0006-margin-gate-uses-ledger-not-wallet-history.md).

Held stock is always recommended for sale at the current front-of-queue price —
the capital is already spent, so it shouldn't sit idle — but **flagged** when its
margin falls short of target. No ESI wallet call is needed: the figure is a
**net margin** (§9's formula, same term — not a new "realised margin", which stays
reserved for the true post-sale figure, §18), with the lot's `acquisition_price`
standing in for `B*`:

```
net margin = (S* − acquisition_price − broker·acquisition_price − broker·S* − tax·S*) / S*
```

**Multi-lot blending.** A type's recommendation is a **quantity-weighted
average** across its lots' acquisition prices — one number, matching "one
recommendation, one sell order." If any lots are seeded (`acquisition_price:
null`, §7), the average is computed over only the priced quantity, with the
unpriced quantity surfaced explicitly (e.g. "margin computed on 630/980 units")
rather than dropped or guessed at.

**Threshold.** Reuses the same configured target net margin that gates buy
recommendations (§9) — a hard filter there, a flag threshold here that never
excludes a sale.

## 13. Allocation

Settled by [Decide the allocation model](https://github.com/mgoodness/eve-trader/issues/5)
and refined by [Decide allocation handling of order granularity](https://github.com/mgoodness/eve-trader/issues/12);
**(v1.1)** extended by [Decide allocation handling of pre-existing open
orders](https://github.com/mgoodness/eve-trader/issues/41). Design rationale:
[ADR 0007](adr/0007-sell-recommendations-get-order-limit-priority.md).

Given a **budget** (required, self-reported ISK) and the skill-derived **order
limit**:

1. **(v1.1)** Every currently-open order — buy or sell, this tool's or not —
   subtracts one from the order limit before this run's recommendations get a
   slot. An `unknown-outcome` lot (§7) reserves nothing — it's absent from
   `/orders/`, so its fate is unresolved, not reserved.
2. **(v1.1)** Every open buy order's current escrow (`price × volume_remain`;
   already-paid broker fees are sunk, not subtracted again) subtracts from the
   stated budget. Open sell orders don't touch budget — no escrow.
3. **(v1.1)** Of the remaining order-limit slots, **sell recommendations claim
   theirs first** — recovering already-spent capital beats deploying new
   capital. If slots run out before every held-stock type gets one, the excess
   land in `Pending` (`order-limit-exhausted`, §11) rather than silently
   breaking "always recommend."
4. On whatever budget and slots remain, run buy allocation unchanged from v1:
   sort buy candidates by **expected daily profit per ISK of committed
   capital**, descending;
5. per candidate, units are capped at `capture rate × 30-day ADV × horizon`
   (default horizon **3 days**; configurable 1/3/7), floored down to the
   nearest 100-unit lot (**v1.1**; fixed, not configurable) so a posted
   order's quantity is always a round number against the in-game UI;
6. **committed capital** per unit = `B* + broker·B* + broker·S*` (escrow + both
   broker fees; sales tax is netted at sale);
7. add candidates while budget and slots allow — but where a whole order does not
   fit the remainder, **fill it partially** rather than skip: buy as many units as
   the remainder affords, again floored to the nearest 100-unit lot — never
   rounded up, since that would exceed either the units cap or the budget
   the floor itself just computed;
8. a partial fill below the **minimum order** (default **1M ISK committed**,
   configurable) is not posted; leave the remainder idle and continue — this
   now also catches a cap under 100 units, which floors to zero and is
   never postable regardless of budget.

Partial filling is the fractional-knapsack optimum for the budget constraint. At
the pilot's real skills and a 150M budget it commits 99% of budget and 20.9M/day,
versus 35% and 11.85M/day for whole-order skipping (pre-v1.1 figures; unaffected
by the reservation steps above, which only shrink the inputs).

## 14. Output contract

Settled by [Prototype the CLI output contract](https://github.com/mgoodness/eve-trader/issues/6);
**(v1.1)** redesigned by [Prototype the inventory-aware CLI output
contract](https://github.com/mgoodness/eve-trader/issues/42) (prototype branch
`prototype/inventory-output-contract`, variant A).

Default is a **dense table**, exact ISK:

**Summary.** Budget and order limit, each split into *reserved by existing
orders* / *available* / *used this run* (§13) — a second run shouldn't feel like
it arbitrarily has less room without being told why.

**Buy recommendations**, sorted by expected daily profit, three
always-distinguishable groups (unchanged from v1):

1. **funded** — the allocated set (partial fills show their reduced units);
2. **not funded by budget** — passed the filters, got zero units (margin,
   EDP/day, capital needed);
3. **excluded by filters** — a count by default; the item list and reasons under
   `--explain`.

**Sell recommendations** (v1.1), sorted by worst margin shortfall first: item,
units (always the full held-unlisted quantity), sell price, net margin (§12,
"X% on Y/Zu" when partially priced), a flag when under target, **net proceeds**
(gross revenue minus sales tax and this order's own broker fee — never netted
against acquisition price, that's net margin's job), and a lot count.
Column widths are sized from the actual row data, not hardcoded — a fixed guess
breaks the moment any cell (e.g. the partial-pricing notation) runs longer than
expected.

**Pending** (v1.1): a count by reason by default; detail under `--explain`.

`--json` emits the stable machine contract:

```
meta  { generated_at, region_id, trade_station, params{…}, fees{broker, sales_tax} }
summary { budget, budget_reserved_by_existing, budget_available, budget_used,
          order_limit, orders_reserved_by_existing, orders_available, orders_used,
          expected_daily_profit }
buy_recommendations[ { type_id, name, best_bid, best_ask, spread, buy_price,
                        net_margin, profit_per_unit, expected_daily_profit,
                        roi_per_day, units, committed_capital, days_to_clear,
                        average_daily_volume, flags } ]
unfunded[ { … } ]
excluded[ { type_id, name, reason } ]
sell_recommendations[ { type_id, name, quantity, sell_price, net_margin,
                         below_target, priced_quantity, unpriced_quantity,
                         lots[ { lot_id, quantity, acquisition_price } ],
                         net_proceeds } ]
pending[ { type_id, name, reason, quantity, order_id, detail } ]
```

`roi_per_day` stays in the JSON for adapters even though the table omits it.

## 15. Authentication

Settled by [Research: ESI SSO for a headless Go CLI](https://github.com/mgoodness/eve-trader/issues/7)
and [Provision the ESI application and CLI token flow](https://github.com/mgoodness/eve-trader/issues/8);
**(v1.1)** scopes expanded by [Provision expanded ESI scopes and re-authorize
the CLI token](https://github.com/mgoodness/eve-trader/issues/36).

OAuth 2.0 **Authorization Code + PKCE** (S256), no client secret, no device flow.
Endpoints `login.eveonline.com/v2/oauth/{authorize,token}`. Scopes:
`esi-skills.read_skills.v1`, `esi-characters.read_standings.v1`,
**(v1.1)** `esi-assets.read_assets.v1`, `esi-markets.read_character_orders.v1`,
`esi-wallet.read_character_wallet.v1`. Redirect is the loopback
`http://127.0.0.1:8000/callback` (verified accepted). The access token is a JWT
valid 20 minutes carrying the character id in `sub`; the refresh token is
long-lived and **rotates — persist the returned one every time**. One
interactive browser round-trip, then refresh forever; an existing narrower-scope
refresh token does not retroactively widen — re-authorization is required when
scopes change (live-confirmed: the portal must have the new scope saved **and**
`login --force` re-run before a widened token is issued).

## 16. Configuration and state

Settled by [Decide the CLI config and state surface](https://github.com/mgoodness/eve-trader/issues/13);
**(v1.1)** the "no user state" line below no longer holds — see [Design the
trading-stock ledger](https://github.com/mgoodness/eve-trader/issues/37).

Defaults live in `~/.config/eve-trader/config.toml`; CLI flags override. Configurable:
budget, target net margin, δ, horizon, minimum order, capture rate, and the filter
thresholds. Read from ESI every run, not configurable: skills, standings, fees,
order limit. The character is implicit in the token — no `--character` flag.

Four disk locations:

- `~/.config/eve-trader/credentials.json` (600) — client id/secret, redirect,
  refresh token;
- `~/.config/eve-trader/config.toml` — defaults;
- `~/.cache/eve-trader/` — the data cache;
- **(v1.1)** `~/.local/state/eve-trader/ledger.json` — the trading-stock ledger
  (§7), the project's first persistent user state.

## 17. Acceptance criteria

Settled by [Decide ex-post validation and acceptance criteria](https://github.com/mgoodness/eve-trader/issues/11);
**(v1.1)** extended for inventory awareness.

v1 acceptance (unchanged):

- every **funded** buy recommendation is postable (`B* > best bid`);
- it clears the target net margin at the pilot's real skills and standings;
- it respects the budget and the 21-order limit;
- the three-way split accounts for every two-sided type
  (`funded + unfunded + excluded`).

**(v1.1) additional acceptance:**

- every sell recommendation's quantity is backed by ledger held-unlisted stock,
  never more than live ESI assets at the trade station confirm (§7's
  clamp-down rule);
- a sell recommendation is never silently dropped for missing its margin
  target — it's flagged instead (§12);
- pre-existing open orders (any origin) are reserved against both the order
  limit and the budget before this run's new recommendations are computed (§13);
- an `unknown-outcome` lot is surfaced in `Pending`, never silently resolved
  either way (§7).

It makes **no** claim that the 20% capture is correct, nor that a seeded lot's
(absent) acquisition price could have been reconstructed.

## 18. v2 / deferred

The spec names the v2 validation hook so a future effort has a target: log each
recommendation (type, price, timestamp); reconcile against wallet-journal trades
and order history; compute **observed capture** (units sold ÷ (ADV × days)) and
**realised margin** (the true post-sale figure — distinct from §12's pre-sale net
margin); calibrate the flat capture constant. A pass = realised margin clears the
target and observed capture lands within a stated band. **(v1.1)** note: broker
fee cannot be reliably tied back to a specific transaction
(`docs/research/esi-wallet-transactions.md`), so this hook will need to decide
whether to approximate fees analytically (as §12 already does) rather than reading
them from the wallet journal.

Also v2: a fill-probability model and per-item δ, relist/undercut alerts, trading
on behalf of other characters as its own distinct feature (separate budgets and
reporting), a manual CLI escape hatch for ledger drift beyond the automatic
clamp-down (§7), and — if the destination is redrawn — other hubs, structure
markets, and hauling.

---

## Decision provenance

| Section | Ticket |
|---|---|
| 4 Locked mechanics, 15 Auth | [Research: ESI SSO](https://github.com/mgoodness/eve-trader/issues/7), [Provision the ESI app](https://github.com/mgoodness/eve-trader/issues/8), [Provision expanded ESI scopes](https://github.com/mgoodness/eve-trader/issues/36) |
| 9 Pricing rule | [Prototype: front-of-queue pricing](https://github.com/mgoodness/eve-trader/issues/2) |
| 8 Filter layer, 10 Ranking | [Decide filtering, ranking, volume capture](https://github.com/mgoodness/eve-trader/issues/4) |
| 7 Inventory ledger | [Design the trading-stock ledger](https://github.com/mgoodness/eve-trader/issues/37), [ledger bootstrap](https://github.com/mgoodness/eve-trader/issues/38), [Research: ESI assets and character orders](https://github.com/mgoodness/eve-trader/issues/34) |
| 11 Recommendation lifecycle | [Redesign the Recommendation model](https://github.com/mgoodness/eve-trader/issues/39) |
| 12 Margin-gate | [Decide margin-gate and cost-basis display](https://github.com/mgoodness/eve-trader/issues/40), [Research: ESI wallet transactions](https://github.com/mgoodness/eve-trader/issues/35) |
| 13 Allocation | [Decide the allocation model](https://github.com/mgoodness/eve-trader/issues/5), [allocation granularity](https://github.com/mgoodness/eve-trader/issues/12), [allocation of pre-existing orders](https://github.com/mgoodness/eve-trader/issues/41) |
| 14 Output | [Prototype: CLI output contract](https://github.com/mgoodness/eve-trader/issues/6), [Prototype: inventory-aware CLI output contract](https://github.com/mgoodness/eve-trader/issues/42) |
| 6 Data pipeline | [Decide the ESI data sync](https://github.com/mgoodness/eve-trader/issues/3), [range coverage model](https://github.com/mgoodness/eve-trader/issues/10) |
| 17 Acceptance | [Decide ex-post validation](https://github.com/mgoodness/eve-trader/issues/11) |
| 16 Config & state | [Decide the CLI config and state surface](https://github.com/mgoodness/eve-trader/issues/13) |

Research: `docs/research/eve-market-mechanics-and-esi.md`,
`docs/research/eveprofits-prior-art.md`, `docs/research/esi-sso-cli.md`,
`docs/research/esi-assets-and-orders.md`, `docs/research/esi-wallet-transactions.md`.
Vocabulary: `CONTEXT.md`. Architecture decisions: `docs/adr/0001`–`0007`.
