# eve-trader v1 — specification

Status: **draft for review**. Assembled from the closed decisions on the
[wayfinder map](https://github.com/mgoodness/eve-trader/issues/1). Every section
cites the ticket that settled it; the locked mechanics are cross-checked against
`docs/research/`.

---

## 1. Summary

`eve-trader` is an on-demand command-line tool. It reads live market data for the
**Heimatar** region over ESI, plus the pilot's character skills and standings, and
recommends — per candidate item — the **buy-order price**, **sell price**, and
**quantity** that place the pilot at the front of the **Rens** order book while
still clearing a target net margin, under a stated budget and order limit.

It recommends; it never places orders.

## 2. Scope

**In scope.** One trade station (Rens VI − Moon 8 − Brutor Tribe Treasury,
`60004588`, in Rens system `30002510`, Heimatar `10000030`); NPC stations only;
one character; a single on-demand run producing a ranked recommendation set.

**Out of scope** (see the map's *Out of scope*). Hauling and regional arbitrage;
the pilot's own regional-range buy orders; Upwell/structure markets; other regions
or hubs; web/TUI/daemon/MCP surfaces; a fill-probability model, an aggression knob
for δ, and stateful order tracking / relist / alerts; the ex-post validation
harness and capture calibration. All are v2 or a different product.

## 3. Domain model

The vocabulary is `CONTEXT.md`; it is normative for this spec. The load-bearing
distinction is the two books:

- **Effective buy book** — every buy order whose range covers the trade station,
  wherever it sits in Heimatar. These are the competing bids, and also the prices
  at which held stock can be sold immediately.
- **Effective sell book** — the sell orders located at the trade station. Sellers
  elsewhere do not compete for the trade station's buyers.

## 4. Locked mechanics

Facts, not choices; sources in `docs/research/eve-market-mechanics-and-esi.md`.

| Thing | Value |
|---|---|
| Broker fee (NPC) | `max(1%, 3% − 0.3%·BrokerRelations − 0.03%·factionStanding − 0.02%·corpStanding)`, charged on **each** order leg at creation, minimum 100 ISK per order |
| Sales tax | `7.5% × (1 − 0.11·Accounting)`, seller side only, deducted at sale |
| Buy-order escrow | 100% of order value, locked at creation (no Margin Trading) |
| Order limit | `5 + 4·Trade + 8·Retail + 16·Wholesale + 32·Tycoon` |
| Range enum | `station`, `solarsystem`, `region`, or `1,2,3,4,5,10,20,30,40` jumps |

**The pilot** (from [Provision the ESI application and CLI token flow](https://github.com/mgoodness/eve-trader/issues/8)):
character `932683762`, **Trade 4, Broker Relations 4, Accounting 3** →
**21 active orders**, **broker 1.8%**, **sales tax 5.025%**. The engine reads
these at runtime; it must not assume max skills.

## 5. Architecture

A **pure engine package** (no I/O, no CLI concerns) and a **thin CLI adapter**.
The engine's public surface is the JSON result (§12); every future surface — web,
TUI, daemon, MCP — is another adapter over the same engine. No web code in v1.

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

The history-dependent filters (§7: min history, min ADV, price band) then run on
those 826. History is cached until 11:05 daily, so this is one pass per day.

**Caching & politeness.** A local disk cache keyed by URL with each route's ESI
TTL: orders **300 s**, history **until 11:05**, jumps **86,400 s**, skills **60 s**,
standings **3,600 s**. Use `If-None-Match`/ETag; refetch *all* pages of a paginated
resource together when page 1 nears expiry; back off on 429/420.

## 7. Filter layer

Settled by [Decide candidate filtering, ranking, and volume capture](https://github.com/mgoodness/eve-trader/issues/4).
Hard exclusions, applied in order, each with a recorded reason (`--explain`):

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

## 8. Pricing rule

Settled by [Prototype the front-of-queue pricing search on live Rens data](https://github.com/mgoodness/eve-trader/issues/2).

```
B* = best bid + δ
S* = best ask − δ
net margin = (S* − B* − broker·B* − broker·S* − tax·S*) / S*
```

with `broker` and `tax` at the pilot's skills and standings. Recommend the item
**only if** `net margin ≥ target`. The target is a **filter** on the
front-of-queue prices; prices are never backed off to manufacture it. **δ is a
configurable step, default 100 ISK** (not 0.01).

Edge cases the implementation must handle, all reproduced in the prototype:

- `2δ ≥ spread` → `B* ≥ S*` → no recommendation (at δ = 100, an item needs a
  spread > 200 ISK);
- fees dominate thin spreads — at the pilot's real rates the round-trip cost is
  8.625%, so the ask must clear well above the bid;
- the 100 ISK minimum broker fee per order, modelled at order sizing;
- thin / manipulated books (the filter layer removes them).

## 9. Ranking

Settled by [Decide candidate filtering, ranking, and volume capture](https://github.com/mgoodness/eve-trader/issues/4).
Candidates are ranked by **expected daily profit**:

```
EDP = net profit per unit × capture rate × 30-day ADV
```

with a **flat capture rate of 20% of 30-day ADV** (configurable). EDP is a daily
*rate*, independent of how many units are posted. The row also exposes net margin,
ROI on escrow, and EDP per order slot. The 20% is the model's largest assumption;
calibration is v2 (§15).

## 10. Allocation

Settled by [Decide the allocation model](https://github.com/mgoodness/eve-trader/issues/5)
and refined by [Decide allocation handling of order granularity](https://github.com/mgoodness/eve-trader/issues/12).

Given a **budget** (required, self-reported ISK) and the skill-derived **order
limit** (each candidate costs **2 orders**, a buy and a sell):

1. sort candidates by **expected daily profit per ISK of committed capital**,
   descending;
2. per candidate, units are capped at `capture rate × 30-day ADV × horizon`
   (default horizon **3 days**; configurable 1/3/7);
3. **committed capital** per unit = `B* + broker·B* + broker·S*` (escrow + both
   broker fees; sales tax is netted at sale);
4. add candidates while budget and slots allow — but where a whole order does not
   fit the remainder, **fill it partially** rather than skip: buy as many units as
   the remainder affords;
5. a partial fill below the **minimum order** (default **1M ISK committed**,
   configurable) is not posted; leave the remainder idle and continue.

Partial filling is the fractional-knapsack optimum for the budget constraint. At
the pilot's real skills and a 150M budget it commits 99% of budget and 20.9M/day,
versus 35% and 11.85M/day for whole-order skipping.

## 11. Output contract

Settled by [Prototype the CLI output contract](https://github.com/mgoodness/eve-trader/issues/6).

Default is a **dense table**, exact ISK, sorted by expected daily profit, with
three always-distinguishable groups:

1. **funded recommendations** — the allocated set (partial fills show their reduced
   units);
2. **not funded by budget** — passed the filters, got zero units (margin, EDP/day,
   capital needed);
3. **excluded by filters** — a count by default; the item list and reasons under
   `--explain`.

`--json` emits the stable machine contract:

```
meta  { generated_at, region_id, trade_station, params{…}, fees{broker, sales_tax} }
summary { recommendations, committed_capital, budget, budget_used,
          orders_used, order_limit, expected_daily_profit, excluded, unfunded }
recommendations[ { type_id, name, best_bid, best_ask, spread, buy_price, sell_price,
                   net_margin, profit_per_unit, expected_daily_profit, roi_per_day,
                   units, committed_capital, days_to_clear, average_daily_volume, flags } ]
unfunded[ { … } ]
excluded[ { type_id, name, reason } ]
```

`roi_per_day` stays in the JSON for adapters even though the table omits it.

## 12. Authentication

Settled by [Research: ESI SSO for a headless Go CLI](https://github.com/mgoodness/eve-trader/issues/7)
and [Provision the ESI application and CLI token flow](https://github.com/mgoodness/eve-trader/issues/8).

OAuth 2.0 **Authorization Code + PKCE** (S256), no client secret, no device flow.
Endpoints `login.eveonline.com/v2/oauth/{authorize,token}`. Scopes: exactly
`esi-skills.read_skills.v1` + `esi-characters.read_standings.v1`. Redirect is the
loopback `http://127.0.0.1:8000/callback` (verified accepted). The access token is
a JWT valid 20 minutes carrying the character id in `sub`; the refresh token is
long-lived and **rotates — persist the returned one every time**. One interactive
browser round-trip, then refresh forever.

## 13. Configuration and state

Settled by [Decide the CLI config and state surface](https://github.com/mgoodness/eve-trader/issues/13).

Defaults live in `~/.config/eve-trader/config.toml`; CLI flags override. Configurable:
budget, target net margin, δ, horizon, minimum order, capture rate, and the filter
thresholds. Read from ESI every run, not configurable: skills, standings, fees,
order limit. The character is implicit in the token — no `--character` flag.

Three disk locations, **no user state**:

- `~/.config/eve-trader/credentials.json` (600) — client id/secret, redirect,
  refresh token;
- `~/.config/eve-trader/config.toml` — defaults;
- `~/.cache/eve-trader/` — the data cache.

## 14. Acceptance criteria

Settled by [Decide ex-post validation and acceptance criteria](https://github.com/mgoodness/eve-trader/issues/11).
v1 acceptance is **functional**, checkable on one run against a live snapshot:

- every **funded** recommendation is postable (`B* > best bid`, `S* < best ask`);
- it clears the target net margin at the pilot's real skills and standings;
- it respects the budget and the 21-order limit;
- the three-way split accounts for every two-sided type
  (`funded + unfunded + excluded`).

It makes **no** claim that the 20% capture is correct.

## 15. v2 / deferred

The spec names the v2 validation hook so the next effort has a target: log each
recommendation (type, `B*`, `S*`, timestamp); reconcile against wallet-journal
trades and order history; compute **observed capture** (units sold ÷ (ADV × days))
and **realised margin**; calibrate the flat capture constant. A pass = realised
margin clears the target and observed capture lands within a stated band.

Also v2: a fill-probability model and per-item δ, stateful order tracking with
relist/undercut alerts, and — if the destination is redrawn — other hubs, structure
markets, and hauling.

---

## Decision provenance

| Section | Ticket |
|---|---|
| 4 Locked mechanics, 12 Auth | [Research: ESI SSO](https://github.com/mgoodness/eve-trader/issues/7), [Provision the ESI app](https://github.com/mgoodness/eve-trader/issues/8) |
| 8 Pricing rule | [Prototype: front-of-queue pricing](https://github.com/mgoodness/eve-trader/issues/2) |
| 7 Filter layer, 9 Ranking | [Decide filtering, ranking, volume capture](https://github.com/mgoodness/eve-trader/issues/4) |
| 10 Allocation | [Decide the allocation model](https://github.com/mgoodness/eve-trader/issues/5), [allocation granularity](https://github.com/mgoodness/eve-trader/issues/12) |
| 11 Output | [Prototype: CLI output contract](https://github.com/mgoodness/eve-trader/issues/6) |
| 6 Data pipeline | [Decide the ESI data sync](https://github.com/mgoodness/eve-trader/issues/3), [range coverage model](https://github.com/mgoodness/eve-trader/issues/10) |
| 14 Acceptance | [Decide ex-post validation](https://github.com/mgoodness/eve-trader/issues/11) |
| 13 Config & state | [Decide the CLI config and state surface](https://github.com/mgoodness/eve-trader/issues/13) |

Research: `docs/research/eve-market-mechanics-and-esi.md`,
`docs/research/eveprofits-prior-art.md`, `docs/research/esi-sso-cli.md`.
Vocabulary: `CONTEXT.md`.
