# Prior art & domain concepts for opportunity filters

Research for adding **filters** to eve-trader that exclude unrealistic or
untradeable Opportunities. Investigated 2026-09-18.

**One-line answer:** eveprofits.com's References page documents a concrete,
copyable filter stack (price-band clamp for gross margin >40%, drop items with
<14 trade-days + >20× high/low swing, drop spreads resting on ≤1 order within 5%
of best, require ≥7 recent trade-days, assume ~20% volume capture); every input
it needs is available from ESI's `history` (`average`/`highest`/`lowest`/
`order_count`/`volume`) and `orders` (`price`/`volume_remain`/`is_buy_order`)
endpoints — but eve-trader's `esi.HistoryPoint` currently drops
`average`/`highest`/`lowest`, which the price-band and manipulation filters need.

Sources are primary throughout: the eveprofits pages themselves, the official
ESI OpenAPI spec + live endpoint responses, and third-party tool/first-party
wiki pages. Where only trader heuristics exist, that is stated.

---

## 1. eveprofits.com — prior art (whole-site crawl)

Site: <https://www.eveprofits.com/> (canonical domain also `evemarketprofits.com`).
Nav = Dashboard / References / Updates. Footer = Legal / Privacy / Email.
Dashboard cards link to seven tools. Data source disclosed on every page:

> "Market data reflects NPC station orders only, fetched via the public ESI
> regional endpoint (`/markets/{region_id}/orders/`). Player-owned structure
> (citadel) orders are not included."
> — <https://www.eveprofits.com/references>

Trade hubs offered: Jita (30000142), Amarr (30002187), Dodixie (30002659),
**Rens (30002510)**, Hek (30002053). Dashboard states "Data updated every 30
minutes" (<https://www.eveprofits.com/dashboard>).

### 1a. Max Margin Opportunities — the directly-relevant tool

Page: <https://www.eveprofits.com/max_margin_page> · Reference:
<https://www.eveprofits.com/references>

**Filter form inputs** (exact labels + tooltip text + default values):

| Input | Default | Tooltip / meaning (quoted) |
|---|---|---|
| Trade Hub | Jita | — |
| Item Name | — | search |
| **Min Daily Volume** | **20** | "remove items that are low movers … Defaults to 20 so dead markets don't dominate; clear it to see everything." |
| Min Gross Margin (%) | — | "minimum gross margin … (before fees, in whole numbers, e.g. 10)" |
| **Max Gross Margin (%)** | **60** | "Hides items whose gross margin is unrealistically high (usually **stale/manipulated sell orders or no real buyer**). Defaults to 60; clear it to see those outliers." |
| Max Sell Price | — | budget cap |
| Number of Results | 25 | max 1000 |

Trading Skills bar: Accounting (default 5) and Broker Relations (default 5),
used to recompute net margin client-side.

**Result columns** (from `static/js/max_margin.js`): Item Description, Buy Price
("highest buy order price … updated every 10 min"), Sell Price ("lowest sell
order price … updated every 10 min"), Net Margin (%), **Avg Daily Volume**
("average of the last **30 days** trading volume"), Estimated Daily Profit.
Default sort: Estimated Daily Profit descending.

**On-page realism note (quoted verbatim):**

> "Results show realistic, tradeable opportunities. Items with manipulated or
> thin trade history, spreads resting on a single order, or under 7 days of
> recent trades are excluded. Profit estimates are after fees and assume you
> capture ~20% of daily volume."
> — <https://www.eveprofits.com/max_margin_page>

**Documented formulae** (<https://www.eveprofits.com/references>, "Max Margin —
Ranking & Realism Filters"):

- Gross margin: `M = (P_s − P_b) / P_s × 100%` (margin on **revenue**, not cost).
- Net profit per unit: `π = P_s − P_b − (P_b × R_b) − (P_s × R_b) − (P_s × R_t)`
  (broker fee on **both** buy and sell orders + sales tax on the sale).
- Broker fee rate `R_b = 3% − (0.3% × Broker Relations)` → **1.5% at V**.
- Sales tax rate `R_t = 7.5% × (1 − 0.11 × Accounting)` → **3.375% at V**.
  Total fee burden ≈ **6.375%** at max skills.
- Estimated daily profit `EDP = π × V_d`, `V_d` = 30-day average daily volume,
  then scaled by the ~20% capture rate. Server ranks at max-skill rates; the UI
  recomputes displayed net margin to your own skills.
- (Confirmed in `static/js/net_margin_calculator.js`: same `R_b`, `R_t`, and
  `netMargin = (profitPerUnit / sellPrice) * 100`.)

**The four realism filters (quoted verbatim — this is the core prior art):**

> **Historical price-band validation.** For high-margin candidates (gross margin
> above 40%), the buy and sell prices are checked against the item's 30-day
> trading range. A price far outside that range — **below 0.75× the lowest daily
> low, or above 1.25× the highest daily high** — is treated as an
> outlier/manipulation and pulled to the band edge before margin is computed, so
> a single mispriced order can't manufacture a spread.

> **Manipulated-history exclusion.** Items whose 30-day history is both thin and
> wildly volatile — **fewer than 14 days with trades and a high-to-low swing
> greater than 20×** — are dropped, since their price history is too unreliable
> to validate against.

> **Thin-book exclusion.** High-margin items whose spread rests on a single
> order — **one order or fewer within 5% of the best bid or the best ask** — are
> dropped, because that quoted spread won't survive the order trading.

> **Minimum history.** At least **7 days of recent trade history** are required
> for an item to appear.

> **Capture rate.** Estimated daily profit assumes you realistically capture
> about **20% of an item's daily traded volume** … rather than 100% of it.

Note (potential inconsistency in the prior art): the on-page copy and the
References page both say EDP is scaled by ~20% capture, but the shipped
`net_margin_calculator.js` computes `profitPerUnit * avgDailyVolume` with **no**
0.2 factor. Treat 20% as the documented intent; the client code may apply it
server-side or not at all.

### 1b. Other eveprofits tools (secondary relevance, useful metric vocabulary)

All from <https://www.eveprofits.com/references> unless noted.

- **Items in Low Supply** (`/low_supply_page`). Filters: Min Daily Volume, Min
  Gross Margin, Min Sell Price, **Max Days of Supply**. Metric **Days of Supply
  `DoS = S_v / V_d`** (current sell volume ÷ avg daily volume); log-weight
  `log10(V_d + 1)` prioritizes active items.
- **High Demand, Low Supply** (`/high_demand_low_supply_page`). Filters: Min Avg
  Daily Volume, Min Gross Margin, Min Sell Price. Ranking
  `0.8·log10(V_d+1) + 0.2·log10(R+1)`, `R` = buy/sell unit ratio. Updates note a
  fixed bug where **zero-volume items floated to the top** via an extreme ratio;
  log scaling now sinks them.
- **Market Anomaly Detection** (`/anomaly_detection_page`). Filters: Anomaly
  Type, Severity, Min Gross Margin, Min Daily Volume, Max Buy Price, Min
  Investment. (Detects "unusual price & volume activity" — dashboard.)
- **Price Movement** (`/price_movement_page`). Filters: Min Avg Daily Volume,
  Today's Gross Margin. Metrics: price-change % = `(P_last_week − P_last_month)
  / P_last_month × 100`, same shape for volume-change %; qualitative
  Correlation; weighted score `log10(V_d+1)`.
- **Trading On a Budget** (`/trading_on_a_budget_page`). Greedy knapsack by
  profit efficiency `E = (P_s − P_b) / P_b`. **Safety filters: Max Margin =
  80%** ("likely indicate scams or data errors"), **Min Buy Price = 1,000 ISK**
  ("filters out bad data and very low-value items"), **Min Daily Volume = 1**.
  Position sizing `U_max = floor(V_d × D)`, D ∈ {1 quick, 3 balanced, 7 deep}.
  Also enforces EVE's 150-order character limit.
- **Hauling Routes / Arbitrage** (`/arbitrage_page`, added Jun 2026). Fill-
  existing-orders model: `π = P_dest × (1 − t) − P_source` (sales tax only, no
  broker fee); rank by profit per m³.

**Update-log confirmations** (<https://www.eveprofits.com/updates>):
- "ESI pagination fix: Market order fetching now correctly reads the **X-Pages
  header** to determine page count, preventing silent truncation."
- Max Margin now ranks on **net** margin because "the 6.5% gross margin filter
  sat right at the break-even point for max-skill traders."
- "Fixed spurious 100% margins: **Items with no buy orders** no longer appear
  with 100% gross margin."

---

## 2. ESI market data available for filters

Current official spec is **OpenAPI 3.1.0** at
<https://esi.evetech.net/meta/openapi.json> (the old `/latest/swagger.json` now
404s and redirects to <https://developers.eveonline.com/api-explorer>).
eve-trader targets `https://esi.evetech.net/latest` (`esi/http.go`), Heimatar
region `10000030`, Rens station `location_id 60004588`.

### 2a. `GET /markets/{region_id}/history` — daily statistics per type

Summary: "List historical market statistics in a region." Required query param
`type_id`. **"This route expires daily at 11:05"** (spec description).

Schema `MarketsRegionIdHistoryGet` (array of):

| Field | Type | Spec description |
|---|---|---|
| `date` | string (date) | "The date of this historical statistic entry" |
| `average` | number (double) | *(no description in 3.1 spec — daily average price)* |
| `highest` | number (double) | *(no description — daily highest price)* |
| `lowest` | number (double) | *(no description — daily lowest price)* |
| `order_count` | integer (int64) | **"Total number of orders happened that day"** |
| `volume` | integer (int64) | **"Total"** (units traded that day) |

All six fields are `required`. Live sample (Tritanium type 34, Heimatar,
2026-09-16): `{"average":3.8,"date":"2026-09-16","highest":3.84,"lowest":2.9,
"order_count":147,"volume":179280232}`; the response held 412 days.

> **Caveat / dead end:** the current 3.1 spec omits descriptions for `average`,
> `highest`, `lowest`. Their meaning (daily volume-weighted average / high / low
> transaction price) is the long-standing ESI semantics and consistent with the
> live data, but is **not** described in the primary spec today — flagged as
> "inferred from field name + live data," not quoted.

**eve-trader gap:** `esi.HistoryPoint` (`esi/gateway.go`) only carries `Date`,
`Volume`, `OrderCount`. The price-band and manipulation filters below need
`average`/`highest`/`lowest`, so the gateway's history fetch (`esi/http.go`
`FetchHistory`) would have to be widened to keep those three fields.

### 2b. `GET /markets/{region_id}/orders` — live order book

Summary: "List orders in a region." Required query `order_type`
(`buy`/`sell`/`all`); optional `page` and `type_id`. Paginate via the
**`X-Pages`** response header (live sample: `x-pages: 73`). `Cache-Control:
public`; live `Expires − Last-Modified = 5 minutes`.

Schema `MarketsRegionIdOrdersGet` (array of) — all fields `required`:

| Field | Type | Notes |
|---|---|---|
| `order_id` | int64 | unique order id |
| `type_id` | int64 | item type |
| `location_id` | int64 | station/structure (Rens NPC = 60004588) |
| `system_id` | int64 | "The solar system this order was placed" (Rens sys 30002510) |
| `volume_total` | int64 | original order size |
| `volume_remain` | int64 | **units still available** — the depth input |
| `min_volume` | int64 | min units per fill (usually 1; matters for buy orders) |
| `price` | double | order price |
| `is_buy_order` | boolean | true = bid, false = ask |
| `duration` | int64 | order lifetime (days) |
| `issued` | date-time | when placed (order-age input) |
| `range` | string enum | `station`,`region`,`solarsystem`,`1`…`40` (buy-order reach) |

Live sample (Heimatar, Rens): `{"duration":90,"is_buy_order":false,"issued":
"2026-09-16T16:37:23Z","location_id":60004588,"min_volume":1,"order_id":
7423944266,"price":26990000.0,"range":"region","system_id":30002510,"type_id":
41218,"volume_remain":4,"volume_total":6}`.

To scope to Rens, filter orders to `location_id == 60004588` (eve-trader already
does this per `esi/gateway.go` doc comment). Region history (2a) is region-wide,
not station-specific — consistent with `CONTEXT.md`'s Heimatar/Rens distinction.

---

## 3. Domain concepts EVE traders use to detect thin / manipulated markets

Primary/first-party sources; trader heuristics flagged where no spec exists.

### (a) Market manipulation / inflated spreads
- **Price-band clamp against history** — the technique eveprofits documents
  (§1a): reject or clamp any live price outside `[0.75× 30-day min low, 1.25×
  30-day max high]`. Rooted in ESI `history.lowest`/`highest`. A single
  mispriced/stale order then can't manufacture a spread.
- **Cap gross margin** — eveprofits Max Margin default **60%**, Trading-on-a-
  Budget **80%**, both explicitly "scams/manipulation/data errors" (§1a/1b).
  Heuristic, but two independent thresholds in the same tool.
- **No-buy-order artefact** — an item with no buy order shows a fake 100% margin;
  eveprofits explicitly patched this (§1b updates). Filter: require both a best
  bid and a best ask (matches eve-trader's Opportunity definition in
  `CONTEXT.md`).

### (b) Thin / illiquid markets
- **Minimum average daily volume** — eveprofits default **20 units/day**
  ("so dead markets don't dominate"). eve-trader's v1 threshold is **10
  units/day** (`CONTEXT.md`). ESI input: `history.volume`.
- **Days of Supply** `DoS = sell_volume / V_d` (eveprofits, §1b) — flags items
  where standing sell depth dwarfs real throughput.
- **Log-weighting volume** `log10(V_d + 1)` (eveprofits) — soft de-prioritise,
  not a hard cut; prevents zero-volume items ranking high.

### (c) Order-book depth (spread on a single order)
- **eveprofits thin-book rule** (§1a): drop if **≤1 order within 5% of the best
  bid or best ask**. ESI input: count `orders` where
  `abs(price − best)/best ≤ 0.05`, per side; optionally sum `volume_remain`.
- **Fuzzwork Market Data** (<https://market.fuzzwork.co.uk/>) exposes, per type
  per region, **"Number of Orders"** and a **"5% Buy Average" / "5% Sell
  Average"** — the volume-weighted average price of orders within 5% of the best
  price. This is the canonical community depth metric (the "5% average" every
  appraisal tool uses) and directly parallels eveprofits' 5% band. Primary
  evidence: the aggregate table columns on the site's front page.
- **Adam4EVE** (<https://www.adam4eve.eu/>) ships dedicated **"Order depth"** and
  **"Orderbook age"** tools plus a **"Margin finder"** and "Hub trade history"
  (nav menu, primary evidence). Order-depth = depth; orderbook-age = staleness
  of resting orders (`issued`/`duration`).

### (d) Too few recent trading days
- **eveprofits minimum-history rule** (§1a): **≥7 days** of recent trades to
  appear; and the manipulation rule requires **≥14 trade-days** to trust the
  price history for validation. ESI input: count `history` days with
  `order_count > 0` (or `volume > 0`) inside the recent window.

### Corroborating first-party mechanics (EVE University wiki, `/Trading`)
<https://wiki.eveuniversity.org/Trading>:
- Broker Relations "reduces the broker fee by 0.3 percentage points per level …
  At level 5 this brings you from **3% to 1.5%**." → confirms eveprofits `R_b`.
- Accounting "reduces the **sales tax by 11% per level from 8% to 3.6%** at level
  5." → **Disagreement with eveprofits**, which uses a **7.5% base → 3.375% at
  V**. The 11%-per-level *structure* matches; the *base rate* differs (8% vs
  7.5%). CCP has changed the base sales-tax rate over time; **verify the current
  base against CCP patch notes before hard-coding** — neither source is dated
  authoritatively here. This is the one unresolved fee number.
- Character order cap: 5 base, up to 305 with all trade skills to V (EVE Uni) —
  eveprofits assumes the common 150 (§1b). Not a quality filter; noted for
  budget/position sizing only.

### Tools that are NOT screeners (scope note)
- **EVE Tycoon** (<https://evetycoon.com/>) is profit-tracking + order
  management (undercut hotkey), not a liquidity screener — no quality filters to
  borrow.
- **EVE-MarketData** — historically an aggregator API; superseded in practice by
  ESI/Fuzzwork/Adam4EVE. Not investigated live (likely defunct); flagged as a
  dead end rather than a source.

---

## 4. Recommended filter definitions (in ESI terms)

Notation: history day `h` has `h.volume`, `h.order_count`, `h.average`,
`h.highest`, `h.lowest`; the recent window is the last `N` days (eve-trader uses
a rolling **14-day** window per `CONTEXT.md`; eveprofits references **30 days**).
`bestBid`/`bestAsk` from Rens `orders` (`location_id == 60004588`).

Each filter cites the prior art it derives from; thresholds are eveprofits'
where one exists, else marked *(heuristic)*.

1. **Both sides present.** Require ≥1 buy order and ≥1 sell order at Rens.
   Prevents the fake-100%-margin artefact. *(eveprofits updates §1b;
   already implied by `CONTEXT.md` Opportunity.)*

2. **Minimum recent trade-days.** Require `count(h : h.order_count > 0) ≥ 7`
   over the window. *(eveprofits "Minimum history", §1a; exact quote "at least 7
   days".)* Tradeoff: raise toward 14 to also satisfy the manipulation-trust
   bar; with a 14-day window, "≥7 of last 14 days traded" is a natural cut.

3. **Minimum average daily volume.** `mean(h.volume) ≥ V_min`. eveprofits uses
   **20**; eve-trader v1 uses **10**. Keep eve-trader's 10 unless you observe
   dead markets dominating, then move to 20. ESI: `history.volume`.

4. **Manipulated-history exclusion.** Drop if the history is *both* thin and
   volatile: `count(h : h.order_count>0) < 14` **AND**
   `max(h.highest)/min(h.lowest) > 20`. *(eveprofits, §1a, exact thresholds.)*
   Needs `highest`/`lowest` — currently missing from `esi.HistoryPoint`.

5. **Price-band clamp for high-margin candidates.** When gross margin > **40%**,
   clamp `bestBid` and `bestAsk` into `[0.75 × min(h.lowest), 1.25 ×
   max(h.highest)]` before recomputing margin; a price outside the band is
   treated as an outlier. *(eveprofits, §1a, exact multipliers.)* Alternative to
   clamping: simply exclude the opportunity. Needs `lowest`/`highest`.

6. **Thin-book / single-order-spread exclusion.** For high-margin candidates,
   drop if either side has **≤1 order within 5% of best**:
   `count(o : o.is_buy_order==true  and (bestBid−o.price)/bestBid ≤ 0.05) ≤ 1`
   **or** the symmetric ask-side count ≤ 1. *(eveprofits, §1a.)* Stronger
   variant *(heuristic, from Fuzzwork's "5% average" depth metric)*: require
   aggregate `volume_remain` within the 5% band ≥ some `X` (e.g. ≥ one day's
   `V_d`) so the quoted spread survives real fills. Uses `orders.price`,
   `orders.volume_remain`, `orders.is_buy_order`.

7. **Sanity cap on gross margin.** Drop opportunities with gross margin above a
   ceiling — eveprofits default **60%** (Max Margin) / **80%** (Budget). Cheap,
   catches stale/scam listings that slip past the band clamp. *(eveprofits,
   §1a/1b.)*

8. **Rank on net, not gross.** Compute `π` after fees (`R_b`=1.5%, `R_t`≈3.375%
   at V) and rank by **net** EDP `π × V_d × captureRate`, `captureRate ≈ 0.20`.
   *(eveprofits, §1a; and its own update noting a gross filter "sat right at the
   break-even point.")* Confirm the sales-tax base against current CCP patch
   notes (see §3 disagreement) before hard-coding 3.375%.

**Not recommended as a hard filter:** a coefficient-of-variation cut
(`stddev(h.volume)/mean(h.volume) < X`). No primary source uses it; eveprofits
instead pairs a thin-days count with a high/low price-swing ratio (#4), which is
better grounded and needs no volume std-dev. Mentioned only because the brief
raised it — treat as *heuristic, unsupported by prior art*.

**Sequencing (eveprofits applies realism filters *before* ranking):** cheap
structural cuts first (#1, #2, #3), then history-trust and price cuts (#4, #5,
#7), then the depth cut (#6) only on surviving high-margin candidates, then rank
(#8).

---

## Sources

- eveprofits: `/max_margin_page`, `/dashboard`, `/references`, `/updates`,
  `/low_supply_page`, `/high_demand_low_supply_page`, `/anomaly_detection_page`,
  `/price_movement_page`, and `static/js/{max_margin,net_margin_calculator}.js`
  (all under <https://www.eveprofits.com/>, fetched 2026-09-18).
- ESI OpenAPI 3.1.0 spec: <https://esi.evetech.net/meta/openapi.json>
  (schemas `MarketsRegionIdHistoryGet`, `MarketsRegionIdOrdersGet`); live
  endpoints `…/latest/markets/10000030/{history,orders}/` fetched 2026-09-18.
- EVE University Wiki, *Trading*: <https://wiki.eveuniversity.org/Trading>.
- Fuzzwork Market Data: <https://market.fuzzwork.co.uk/>.
- Adam4EVE: <https://www.adam4eve.eu/> (nav: Margin finder, Order depth,
  Orderbook age, Hub trade history).
- EVE Tycoon: <https://evetycoon.com/>.
- Repo context: `CONTEXT.md`, `esi/gateway.go`, `esi/http.go`.
