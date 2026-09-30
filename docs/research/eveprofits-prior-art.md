# Prior art: eveprofits.com — a station-trading recommendation engine

**Question.** What does https://www.eveprofits.com do, exactly — its tool inventory, its
documented/implied station-trading formulae, its stated assumptions and defaults, which EVE
skills it models and how, and how it sources and ages market data?

**Artifact location.** `docs/research/eveprofits-prior-art.md` (no `docs/research/`
convention existed in this repo; `docs/` previously held only `docs/agents/`).

**All sources read on 2026-09-30** (UTC). Market data cited from the live site is a snapshot
from that date; the site itself reports its last pipeline run on the dashboard.

**Method.** Read the site's own HTML, its own client-side JavaScript, its own public
JSON endpoints (plain `POST` requests — read-only), the developer's first-party forum/Reddit
posts, and CCP-operated sources (ESI, the SDE-as-served-by-ESI, CCP Support articles, CCP
news) to check every number the site claims. Where the site is silent, that is recorded as
**not found**. Inferences are labelled as inferences.

---

## 0. Source inventory

| Tag | Source | What it is |
|---|---|---|
| `[REF]` | https://www.eveprofits.com/references | The site's own "References and Formulas" page — the primary formula document |
| `[UPD]` | https://www.eveprofits.com/updates | Site changelog (Nov 2024 → Jun 2026) — contains formula changes and bugs fixed |
| `[DASH]` | https://www.eveprofits.com/dashboard | Dashboard; carries the "Data updated every 30 minutes / Last run" line |
| `[SITEMAP]` | https://www.eveprofits.com/sitemap.xml | Canonical page list |
| `[JS]` | `https://www.eveprofits.com/static/js/net_margin_calculator.js` (+ `max_margin.js`, `trading_on_a_budget.js`, `arbitrage.js`, `anomaly_detection.js`, `price_movement.js`, `low_supply.js`, `high_demand_low_supply.js`) | The site's actual client-side calculation code |
| `[API]` | `POST /max_margin_page`, `/trading_on_a_budget_page`, `/arbitrage_page`, `/arbitrage_lane_detail`, `/anomaly_detection_page`, `/price_movement_page`, `/low_supply_page`, `/high_demand_low_supply_page` | The site's own JSON endpoints (returned raw JSON observed) |
| `[FAQ-2024]` | https://forums.eveonline.com/t/introducing-eve-profits/461397 | Developer (Bobby_Booche) launch post, 2024-09-14 |
| `[FAQ-2025]` | https://www.reddit.com/r/Eve/comments/1p3zjgr/i_applied_20_years_of_fortune_500_retail/ | Developer walkthrough, 2025-11-22 — anomaly thresholds, "no SSO" statement |
| `[FAQ-2026]` | https://www.reddit.com/r/Eve/comments/1u5rl8w/eveprofitscom_update_your_feedback_became/ | Developer update post, 2026-06-14 |
| `[ESI-TYPES]` | `https://esi.evetech.net/latest/universe/types/{id}/?datasource=tranquility&language=en` | CCP-served skill descriptions (typeIDs 16622, 3446, 3443, 3444, 16596, 18580, 16595, 3447, 16598) |
| `[ESI-MKT]` | `https://esi.evetech.net/latest/markets/{region_id}/orders/` and `.../history/` | CCP's public market endpoints + their live `Cache-Control`/`Expires`/`X-Pages` headers |
| `[CCP-FEES]` | https://support.eveonline.com/hc/en-us/articles/203218962-Broker-Fee-and-Sales-Tax | CCP Support, updated 2026-06-02 — broker fee + sales tax formulae |
| `[CCP-ORDERS]` | https://support.eveonline.com/hc/en-us/articles/203218932-Buy-and-Sell-Orders | CCP Support — order matching + **relist** fee formula |
| `[CCP-100ISK]` | https://www.eveonline.com/news/view/broker-relations | CCP news, 2020-02-24 — "Both types of fee will have a minimum charge of 100 ISK" |
| `[ESI-DOCS-STRUCT]` | https://github.com/esi/esi-docs/blob/master/docs/scenarios/structure_markets.md | CCP-maintained ESI docs: what `/markets/{region_id}/orders/` does with structure orders |

Dead ends (recorded as findings): there is **no public source repository** for eveprofits.com
(a code search returned only unrelated projects; the developer describes the site as
"vibe coded … Python on the backend" but has not open-sourced it — `[FAQ-2025]`).
`https://www.eveprofits.com/results_table.html` is 404 and is `Disallow`ed in
https://www.eveprofits.com/robots.txt. ESI's `swagger.json`/`openapi.json` now 404;
`https://esi.evetech.net/ui/` 301-redirects to https://developers.eveonline.com/api-explorer.
`/universe/stations/30000142/` returns `{"error":"Station not found"}` — the IDs the site uses
in its trade-hub dropdowns (30000142, 30002187, 30002659, 30002510, 30002053) are **solar
system IDs** (Jita, Amarr, Dodixie, Rens, Hek), not station IDs.

---

## 1. Tool inventory

The dashboard renders **seven** analysis cards `[DASH]`; the nav lists the same seven pages plus
References/Updates. All are open, no login.

| # | Tool | Page / endpoint | What it computes (as documented) |
|---|---|---|---|
| 1 | **Max Margin Opportunities** | `/max_margin_page` | Highest-margin single-station flips per hub; ranks by Estimated Daily Profit after fees, with realism filters `[REF]` |
| 2 | **Hauling Routes (Arbitrage)** | `/arbitrage_page`, `/arbitrage_lane_detail` | Jita-anchored inter-hub arbitrage: buy a *sell order* at hub A, haul, sell into a *buy order* at hub B; cargo-limited greedy knapsack by ISK/m³ `[REF]` |
| 3 | **Market Anomaly Detection** | `/anomaly_detection_page` | Statistical outlier detection bucketed as Price Spike / Price Drop / Volume Surge / Spread Expansion / Market Manipulation `[DASH]`, `[FAQ-2025]` |
| 4 | **Items in Low Supply** | `/low_supply_page` | Days of Supply + margin + log-volume weighting, for restocking/industrial plays `[REF]` |
| 5 | **High Demand, Low Supply** | `/high_demand_low_supply_page` | Buy-units vs sell-units ratio, log-weighted against volume `[REF]` |
| 6 | **Trading On a Budget** | `/trading_on_a_budget_page` | Greedy-knapsack portfolio sized to a user ISK budget and a risk profile (1/3/7 days of supply) `[REF]` |
| 7 | **Price Movement Analysis** | `/price_movement_page` | 7-day vs 30-day SMA price/volume change and sign of price↔volume correlation `[REF]`, `[FAQ-2026]` |

**Explicitly *not* offered** (scope statement, `[FAQ-2025]`): "The scope of the project overall is
in station trading." The developer originally listed "Something to do with regional arbitrage" as a
*wanted* feature; hauling arrived in Jun 2026 `[UPD]`. There is **no** blueprint/invention,
reprocessing, industry, or contract calculator anywhere in the nav, sitemap, or dashboard
(`[SITEMAP]`, `[DASH]`). That is: **not found**.

### 1.1 Per-tool inputs (from each page's form) and outputs (from `[JS]` / `[API]`)

**Max Margin** — inputs: trade hub (system ID, default Jita `30000142`), item search, Min Daily
Volume (HTML default `20`), Min Gross Margin %, **Max Gross Margin % (HTML default `60`)**, Max
Sell Price, Number of Results (default `25`) `[max_margin_page HTML]`.
Outputs: Item, Buy Price (highest buy order in hub), Sell Price (lowest sell order in hub), Net
Margin %, Avg Daily Volume (30-day), Estimated Daily Profit `[JS max_margin.js]`.

**Items in Low Supply** — inputs: hub, item search, Min Daily Volume, Min Gross Margin %, Min Sell
Price, Max Days of Supply, Number of Results (25). Outputs: Item, Avg Daily Volume, Buy Price,
Sell Price, Net Margin, Total Buy Units, Total Sell Units, Days of Supply `[JS low_supply.js]`.

**High Demand, Low Supply** — inputs: hub, item name, Min Avg Daily Volume, Min Gross Margin %,
Min Sell Price, Number of Results (25). Outputs add **Buy/Sell Ratio** = total buy units ÷ total
sell units `[JS high_demand_low_supply.js]`; observed `"Buy/Sell Ratio":"0.62"` for Compressed
Veldspar with 1,625,958,543 buy units ÷ 2,629,044,114 sell units `[API]` ✓.

**Price Movement** — inputs: hub, item name, Min Avg Daily Volume, Min Gross Margin %, Number of
Results (25). **No skills bar** — removed per `[UPD]` ("not applicable for gross margin analysis").
Outputs: Item, Avg Daily Volume, Today's Margin %, Avg Price (Last Month), Avg Price (Last Week),
Price Change %, Volume Change %, Correlation `[JS price_movement.js]`. Observed JSON also carries
an internal `weighted_score` `[API]`.

**Trading On a Budget** — inputs: hub (default Jita), Budget (required, ISK), Risk Tolerance
(`conservative` = "Quick Flip (1 day inventory)", `moderate` = "Balanced (3 days inventory)",
`aggressive` = "Deep Stock (7 days inventory)"; default `moderate`), Min Net Margin % (HTML default
`10`), plus the skills bar `[trading_on_a_budget_page HTML]`. Outputs: basket rows with Units to
Buy, Buy/Sell Price, Net Margin %, Total Cost, Expected Profit; summary with overall return,
budget utilisation %, `hit_order_limit`, `max_orders` `[API]`.

**Hauling Routes** — inputs: Cargo Capacity m³ (HTML default `12,000`), Min Net Margin % (default
`5`), Min Profit / Unit (default `0`), optional item search. **No skills bar on this page**
(`[arbitrage_page HTML]` — only hub-less skill selects exist elsewhere), and the JS states
"Server returns final (max-skill) numbers, so this file only formats + renders" `[JS arbitrage.js]`.
Outputs (lane level): Trade Lane, Total Net Profit, Total Cost, ROI, Capacity Used, Items, Best
Profit/m³. Drill-down (item level): Units, Buy @ (Source), Sell @ (Dest), Total m³, Total Cost,
Net Profit, Margin %, Profit/m³ `[JS arbitrage.js]`.

**Anomaly Detection** — inputs: hub (Jita/Amarr/Dodixie/Rens/Hek), Anomaly Type (All / Price Spike /
Price Drop / Volume Surge / Spread Expansion / Market Manipulation), Severity (All/High/Medium/Low),
Min Gross Margin %, Min Daily Volume, Max Buy Price, Min Investment, Number of Results `[HTML]`.
Outputs: Item, Trade Hub, Anomaly Type, **Anomaly Metric** (e.g. `"7.2x Normal"`), Buy/Sell Price,
Net Margin %, **Order Depth** (e.g. `"10 Buy / 178 Sell"`), Avg Daily Volume, Total Profit
Potential, Severity `[JS anomaly_detection.js]`, `[API]`.

---

## 2. Station-trading formulae

Everything below is quoted or transcribed from the site's own References page `[REF]` and its
own JavaScript `[JS]`, with CCP primary sources for the game constants.

### 2.1 Gross margin

```
M = (P_s − P_b) / P_s × 100%
```
`P_b` = buy price, `P_s` = sell price. `[REF]` (stated identically in the Max Margin and Price
Movement sections.)

The *displayed* "Buy Price"/"Sell Price" are described in the page tooltips as "The highest buy
order price in the selected trade hub" / "The lowest sell order price in the selected trade hub"
`[JS max_margin.js]` — i.e. **best bid and best ask**, no depth walking.

### 2.2 Fee rates (the site's core assumption)

```
R_b (broker fee rate)  = 3% − (0.3% × Broker Relations level)      [REF, JS]
R_t (sales tax rate)   = 7.5% × (1 − 0.11 × Accounting level)      [REF, JS]
```

`[JS net_margin_calculator.js]`:
```js
const brokerRate   = 0.03 - (0.003 * brokerRelationsLevel);
const salesTaxRate = 0.075 * (1 - 0.11 * accountingLevel);
```

At level 5/5: `R_b = 1.5%`, `R_t = 3.375%`, "~6.375% total fees" `[REF]`, `[UPD]`.

Both constants match CCP exactly:
* CCP Support: broker fee "Starting at **3%** of the order value, the skill 'Broker Relations'
  reduces the fee by **0.3% per level**" `[CCP-FEES]`.
* CCP Support: sales tax "start at **7.5%** of the sales price. This percentage can be reduced
  down to **3.37%** through the 'Accounting' skill" `[CCP-FEES]`; and the CCP-served skill text
  for Accounting (typeID 16622) reads "Each level of skill reduces sales tax by 11%. Sales tax
  starts at 7.5%." `[ESI-TYPES]`.

### 2.3 Fee amounts and net profit per unit

```
F_b,buy = P_b × R_b        (broker fee on the buy order you place)
F_b,sell = P_s × R_b       (broker fee on the sell order you place)
F_t     = P_s × R_t        (sales tax, seller side)
[REF]
```

### 2.4 Net margin — **the site gives two different denominators**

`[REF]` §"Net Margin Calculations":
```
C = P_b + F_b,buy + F_b,sell + F_t
π = P_s − C
M_net = π / C × 100%          ← cost basis
```

`[REF]` §"Trading On a Budget":
```
M_net = (P_s − P_b − F_b − F_s − T_s) / P_s × 100%    ← revenue basis
```

`[JS net_margin_calculator.js]`, with the comment *"CORRECT - Margin is profit as percentage of
revenue (sell price), not cost"*:
```js
const costPerUnit  = buyPrice + buyBrokerFee + sellBrokerFee + salesTax;
const profitPerUnit = sellPrice - costPerUnit;
const netMargin = (profitPerUnit / sellPrice) * 100;
```

`[UPD]` (Nov 25) settles it: *"Correct Formula: Net Margin = (Sell − Buy − Fees) / Sell × 100%"*,
after "net margin was calculated as markup (profit/cost) … resulting in impossible values like
296%."

**Verdict:** the shipped code and the changelog use **π / P_s**. The `π / C` passage in the
References page's "Net Margin Calculations" section is stale/incorrect documentation. A
caller reimplementing from the References page will get a different (higher) number than the site
displays. (Also note `[UPD]` says "all net margin values now correctly show 0-100% range" — with
the `/C` form that claim is not even true arithmetically.)

### 2.5 Estimated Daily Profit (EDP) — three different definitions, verified against the API

| Where | Definition |
|---|---|
| `[REF]` Max Margin § | `EDP = π × V_d` where π is *net* profit per unit, "adjusted by the capture rate" |
| `[REF]` capture-rate note | "assumes you realistically capture about **20%** of an item's daily traded volume" |
| `[FAQ-2025]` | "The EDP is calculated by assuming you buy and sell every unit in the system… Noble Metals margin is 22% and margin ISK = 1.54. So 1.54 × 56,585,044 = 87,140,968 per day." (gross spread × volume) |
| Server JSON field `"Estimated Daily Profit"` | **(P_s − P_b) × V_d × 0.20** — gross, not net |
| Browser-rendered table cell | **π_net × V_d** — net, but *no* 20% capture factor |

Verified against `[API]` on 2026-09-30 for Jita (`trade_hub=30000142`):

| Item | Buy | Sell | ADV | JSON "Estimated Daily Profit" | (S−B)×ADV×0.2 | net π × ADV (what the table shows) |
|---|---|---|---|---|---|---|
| Logic Circuit | 1,540,000 | 1,768,000 | 70,081 | 3,195,689,040 | 3,195,693,600 | ≈ 8.32e9 |
| HyperCore | 251,300 | 291,000 | 279,938 | 2,222,710,896 | 2,222,707,720 | ≈ 5.80e9 |
| Hydrocarbons | 418.90 | 623.50 | 15,701,804 | 642,517,820 | 642,515,815 | ≈ 2.64e9 |

The JSON column matches gross×volume×0.20 to within rounding on every row tested; the rendered
cell (computed in `[JS net_margin_calculator.js]` as `result.profitPerUnit * avgDailyVolume`)
matches net×volume with **no** capture factor. So: the documented formula matches **neither**
the stored value **nor** the displayed value.

### 2.6 Realism filters (Max Margin only) — `[REF]` §"Max Margin — Ranking & Realism Filters"

* **Historical price-band validation.** For candidates with gross margin > 40%, prices outside
  `[0.75 × lowest daily low, 1.25 × highest daily high]` over the item's 30-day range are "pulled
  to the band edge before margin is computed."
* **Manipulated-history exclusion.** Drop items with "fewer than 14 days with trades **and** a
  high-to-low swing greater than 20×."
* **Thin-book exclusion.** Drop high-margin items whose spread rests on "one order or fewer within
  **5%** of the best bid or the best ask."
* **Minimum history.** "At least **7 days** of recent trade history are required."
* **Capture rate.** 20% of daily traded volume (see §2.5).

These filters are **Max Margin only**; see §6.3 for the arbitrage page producing 1,860,860 % margins
with no such guard.

### 2.7 Undercut / overbid / relist mechanics — **not modelled at all (not found)**

The site contains **no** reference to 0.01-ISK undercutting, overbidding, order re-pricing, or
relist fees. A case-insensitive search of every scraped page and every served JS file for
`undercut|overbid|0\.01|relax|deviate` returns **no relevant hits** (only unrelated ISK-formatting
matches). The engine prices a flip as "best bid in, best ask out" and stops there.

For contrast, CCP's own model (which the prior art ignores) is:
* Order matching, per CCP Support: a new sell order is filled against "the buy order with the
  highest price … at the price the seller offered"; a new buy order is filled against "the sell
  order with the lowest price … and the seller will receive the price per unit offered by the buy
  order" `[CCP-ORDERS]`. Being *best* therefore requires beating the current best by the minimum
  tick (0.01 ISK) or winning the time priority.
* Relist fee, per CCP Support: `Fee = max(0, BR × (P2 − P1)) + (1 − RD) × BR × P2`, where `RD` is
  the Relist Discount from Advanced Broker Relations `[CCP-ORDERS]`.

So a caller building "recommend buy/sell order prices" cannot take these formulae from eveprofits —
eveprofits only tells you a *spread exists*, not what price to post.

### 2.8 Other documented formulae

**Days of Supply** (Low Supply) `[REF]`:
```
DoS = S_v / V_d          S_v = current sell volume (units), V_d = avg daily volume
Log Volume Weight = log10(V_d + 1)
```
Observed: Compressed Copious Cobaltite with Total Sell Units `49` and ADV `10,999` → `"Days of
Supply":"0.00"` (49/10999 = 0.0045) `[API]` ✓.

**High Demand, Low Supply score** `[REF]`:
```
Weighted Score = 0.8 × log10(V_d + 1) + 0.2 × log10(R + 1)
R = total buy units / total sell units
```

**Price Movement** `[REF]`:
```
M  = (P_s − P_b) / P_s × 100
P_c = (P_last_week − P_last_month) / P_last_month × 100
V_c = (V_last_week − V_last_month) / V_last_month × 100
Correlation = sign-only: "Positive" if price and volume move together, else "Negative"
Weighted Score = log10(V_d + 1)
```
Observed against `[API]`: Tritanium `avg_price_last_month 3.9`, `avg_price_last_week 3.81`,
`price_change_pct −2.22` ((3.81−3.9)/3.9 = −2.31 %; the site reports −2.22 %, i.e. a slightly
different averaging — see §6.4) and `correlation "Negative"`; Compressed Veldspar `margin 6.76285`
matches `(11.09 − 10.34)/11.09` from the High-Demand page's own Max Buy/Min Sell prices ✓.

**Trading On a Budget** `[REF]`:
```
E      = (P_s − P_b) / P_b                     profit efficiency (profit per ISK invested)
U_max  = floor(V_d × D)                        D ∈ {1 (Quick Flip), 3 (Balanced), 7 (Deep Stock)}
P_expected = π_net × U
```
Algorithm: rank by `E` descending, greedily add each item if it fits the remaining budget,
**skip** (don't stop) if it doesn't, halt at budget exhaustion or **150** orders `[REF]`, `[UPD]`.
Safety filters stated: `Max Margin = 80%`, `Min Buy Price = 1,000 ISK`, `Min Daily Volume = 1`
`[REF]`. Verified in `[API]`: request with `budget=1,000,000,000,000` returns exactly
`{"hit_order_limit": true, "max_orders": 150, "num_items": 150}` and `"budget_utilized_pct":
1.212` — the order cap is real and returns 150 rows.
Verified position sizing: Compact Thermal Shield Amplifier, ADV `403`, `moderate` (3 days) →
`"Units to Buy": 1209` = floor(403 × 3) ✓.

**Hauling Routes** `[REF]`:
```
π            = P_dest × (1 − t) − P_source      t = sales tax rate (3.375% at Accounting V)
M_net        = π / P_source × 100%              (cost basis, unlike the trading pages)
Profit/m³    = π / V_unit
```
Verified in `[API]` lane detail: Clone Soldier Trainer Tag, source 1,200,000, dest 1,478,000,
units 1 → `net_profit 228,117.5` = 1,478,000 × (1 − 0.03375) − 1,200,000 ✓; `net_margin_pct
19.00979` = 228,117.5 / 1,200,000 ✓; `profit_per_m3 2,281,175` = 228,117.5 / 0.1 m³ ✓.

**Anomaly Detection** — **not documented on the References page at all (not found)**. The only
first-party statement of thresholds is `[FAQ-2025]`:
* Price spikes/drops: "brought forward if the change in the 7 day avg price is +/- 20% of the mean".
* Spread expansion: "looks at the avg ratio of the buy and sell prices vs the 7d average and brings
  forward any that are greater than **1.5** the trend".
* Method: "It uses some fun math and ML to identify market anomalies and categorize them into
  buckets" — no algorithm, feature set, or severity cut-offs are published.

---

## 3. Stated assumptions, defaults and worked examples

**Fees / skills**
* Default skill levels in the UI: **Accounting = 5, Broker Relations = 5** (the `<option value="5">`
  is `selected` for both, desktop and mobile) `[the tool pages' HTML]`; persisted in browser
  `localStorage` under `eve_accounting_level` / `eve_broker_relations_level` `[JS
  net_margin_calculator.js]`.
* Server-side ranking baseline: "Rankings use max-skill defaults (Accounting V + Broker Relations V,
  ~6.375% total fees) as the baseline — if a trade isn't profitable at max skills, it won't appear
  in results" `[UPD]`, restated in `[REF]`.
* 100 ISK minimum broker fee: acknowledged and **deliberately excluded** — "Broker fees have a
  100 ISK minimum per order (not per unit). This primarily affects single-unit trades of low-value
  items and is not factored into the per-unit margin calculations shown on this site" `[REF]`.
  (CCP confirms the minimum exists: "Both types of fee will have a minimum charge of 100 ISK"
  `[CCP-100ISK]`.)

**Ranking / selection**
* Margin *filters* are on **gross** margin, because gross is "for clarity"; net margin is recomputed
  live in the browser from the user's skills `[UPD]`, `[REF]`. Filter labels say "Min Gross Margin
  (%)" `[UPD]`.
* Before Jun 2026 the Max Margin page used "the 6.5% gross margin filter [which] sat right at the
  break-even point for max-skill traders" `[UPD]` — i.e. the pre-June default minimum was ~6.5 %.
* Max Margin result ordering: "Weighted Score Selection: Items selected by balanced ranking (volume
  + margin), then sorted by profit" `[UPD]`; the browser table is then sorted by its own computed
  Estimated Daily Profit column, descending `[JS max_margin.js]`.
* Anomaly page is "⚡ Lightning-fast precomputed results" (precomputed server-side, not live)
  `[DASH]`, `[JS anomaly_detection.js]` ("Loading precomputed anomaly data…").

**Worked examples actually published**
* `[REF]` §"Example at max skills (5/5)": "Broker Fee Rate: 1.5%", "Sales Tax Rate: 3.375%
  (7.5% × 0.45)".
* `[UPD]` Nov 25: "At max skills (level 5): 1.5% broker fee + 3.375% sales tax = ~6.375% total
  fees."
* `[FAQ-2025]`: "Noble Metals margin is 22% and margin ISK = 1.54. So 1.54 × 56,585,044 =
  87,140,968 per day." and "There are only 36,831 units for sale and an Avg Daily Volume of
  216,994. So if the demand holds, there is only 0.17 days of supply" (36,831/216,994 = 0.1697) ✓.
* `[FAQ-2024]`: "Rich Plagioclase has 125× the # of buy units vs sell units"; "for PLEX the volume
  has decreased by 20%, and the price has increased by 0.49%".

**Number of orders / capital**
* Trading On a Budget: "**150** active market orders (EVE Online limit per character)"
  `[REF]`; "150 Order Limit: Respects typical trained trader capacity (**not exposed in UI**)"
  `[UPD]`. The API exposes it as `max_orders: 150`.
* No order-count limit is modelled anywhere else (Max Margin ignores it).
* **Buy-order escrow / collateral: not modelled — not found.** The word "escrow" does not appear
  on any page or JS file. "Total Cost" and "Total Investment" are simply `buy price × units`
  (`[JS trading_on_a_budget.js]`, verified: 5,903 × 1209 = 7,136,727 = the reported `total_cost_raw`).
  The game mechanic (buy orders escrow the full order value, reduced by the Margin Trading skill)
  is out of scope for the site; there is no mention of Margin Trading.

---

## 4. EVE skills the engine accounts for

**Only two skills are modelled: Accounting and Broker Relations.** They are entered by hand —
**there is no ESI SSO and no character import**. `[FAQ-2025]`: "There is no SSO and also nothing
with Sales Tax or Broker fees built in, so you'll have to estimate those on your own." (The second
clause was true in Nov 2025; the fee engine landed in Nov 2025 `[UPD]`. The "no SSO" state persists
— no login/scope/character references exist anywhere in the site's markup or JS, and the seven
analysis pages expose only two `<select>` dropdowns each.) The privacy policy's line about
"Game-related data that you choose to share with us through the EVE Online API"
(https://www.eveprofits.com/privacy_page) is not backed by any SSO flow present on the site.

| Skill | TypeID | CCP-served bonus text `[ESI-TYPES]` | How eveprofits uses it |
|---|---|---|---|
| **Accounting** | 16622 | "Each level of skill reduces sales tax by 11%. Sales tax starts at 7.5%." | `R_t = 7.5% × (1 − 0.11 × level)`; subtracted as `F_t = P_s × R_t` on every sale. Level ∈ [1,5], clamped in JS. |
| **Broker Relations** | 3446 | "Each level of skill subtracts a flat 0.3% from the costs associated with setting up a market order in a non-player station, which usually come to 3% of the order's total value. **This can be further influenced by the player's standing towards the owner of the station where the order is entered.**" | `R_b = 3% − (0.3% × level)`; charged on **both** the buy order (`P_b × R_b`) and the sell order (`P_s × R_b`). |

### 4.1 Skills that *should* move these numbers but are not modelled

| Skill | TypeID | Effect per CCP `[ESI-TYPES]` | Modelled? |
|---|---|---|---|
| **Trade** | 3443 | "Active buy/sell order limit increased by 4 per level" | **No.** Only the hardcoded 150 in Trading On a Budget. |
| **Retail** | 3444 | "Each level raises the limit of active orders by 8" | No |
| **Wholesale** | 16596 | "…by 16" | No |
| **Tycoon** | 18580 | "…by 32" | No |
| **Daytrading** | 16595 | "Allows for remote modification of buy and sell orders…" | No (station trading doesn't need it) |
| **Visibility** | 3447 | Remote buy-order range | No |
| **Marketing** | 16598 | Remote sell range | No |
| **Procurement** | 16594 | Remote buy orders | No |

**The order-count "150" is not derivable from any CCP source.** The base is 5 orders and the max
with all four order-count skills at V is `5 + 20 + 40 + 80 + 160 = 305`; the site's own
characterisation ("EVE Online limit per character") is therefore inaccurate — 150 is between the
base and the cap and is described elsewhere as "typical trained trader capacity" `[REF]`, `[UPD]`.
The per-level increments above are CCP-served; the base of 5 and the 305 cap are not in the ESI
type description (`[ESI-TYPES]` descriptions list only the per-level deltas) — treat 150 as the
site's own assumption, not a game rule.

**Standings, the other half of the broker-fee formula, are ignored.** CCP's published formula is

```
Broker% = 3% − (0.3% × BrokerRelationsLevel) − (0.03% × FactionStanding) − (0.02% × CorpStanding)
```
with a floor of 1 % `[CCP-FEES]`. The CCP skill text for Broker Relations flags the standing
component explicitly `[ESI-TYPES]`. eveprofits drops both standing terms: a perfect-standing
trader (1 % broker) is modelled by the site as paying 1.5 %, and the site's "$R_b$" is silently
station-owner-blind.

**Also not modelled (game mechanics affecting realised margin):** relist/price-change fees and the
Advanced Broker Relations relist discount `[CCP-ORDERS]`; Upwell-structure broker fees, which are
owner-set and *unaffected by Broker Relations* (`[CCP-FEES]`: "The Broker Relations skill does not
apply on orders on Upwell structures"; owner-set minimum 1 % per `[CCP-100ISK]`); and Margin
Trading / escrow.

**PLEX** is handled specially: "PLEX (type_id 44992) is now fully supported across all pages. PLEX
moved to a dedicated region (19000001 - PLEX Vault) in July 2025, and the system now correctly
fetches market data from this special region." `[UPD]`. Cross-check: region `19000001` exists in
ESI and serves PLEX orders (`GET /markets/19000001/orders/?type_id=44992` returns 1,000 orders on
the first page), but ESI's `/universe/regions/19000001/` returns the name **"GPMR-01"**, not
"PLEX Vault" `[ESI live, 2026-09-30]`.

---

## 5. Market data: sourcing and freshness

**Sourcing — stated.** `[REF]` §"Data source note": "Market data reflects NPC station orders only,
fetched via the public ESI regional endpoint (`/markets/{region_id}/orders/`). Player-owned
structure (citadel) orders are not included." `[REF]` also says the pipeline is "a combination of
SQL queries and Python-based calculations". `[FAQ-2025]`: "This tool uses the CCP ESI endpoints to
consume market data. It's really two categories: Historic and Current market data."

Region IDs and hubs cross-check against ESI: Jita → The Forge `10000002`, Amarr → Domain
`10000043`, Dodixie → Sinq Laison `10000032`, Rens → Heimatar `10000030`, Hek → Metropolis
`10000042` — the lane labels returned by the site name exactly these regions, e.g.
`"Hek (Metropolis) -> Jita (The Forge)"` `[API]`. The dropdown *values* are solar-system IDs
(Jita = `30000142`, etc.).

**Freshness.**
* Current/orders data: **every 30 minutes.** `[DASH]`: "Data updated every 30 minutes | Last run:
  … UTC". `[FAQ-2025]` and `[FAQ-2026]`: "The current market data is ingested every 30 min" /
  "updated every 30 minutes".
* Historic data (30-day volume/price history): **once a day, about an hour after downtime**
  `[FAQ-2025]`; `[FAQ-2024]` says the same ("Market_Trends are updated daily just after down-time").
* The **first** version ran hourly: "Market_Orders and the resulting calculations are done hourly,
  on the hour. All the ESI pulls and custom queries take about 14 min to run" `[FAQ-2024]` — so
  the 30-minute cadence is a post-2024 change.
* **Conflicting in-app claim:** every result-page tooltip says "…updated every **10 min**"
  (`[JS max_margin.js]`, `low_supply.js`, `high_demand_low_supply.js`). These tooltips are stale
  relative to the dashboard and the developer's posts; treat 30 min as the operative figure.
* ESI's own freshness bounds the pipeline: `GET /markets/{region_id}/orders/` returns
  `Cache-Control: public`, `Expires` ≈ 5 minutes after `Last-Modified`, and `X-Pages: 405` for The
  Forge with `order_type=all` `[ESI-MKT, 2026-09-30 16:00 UTC]`; the rate-limit group is
  `market-order` with `X-RateLimit-Limit: 12000/15m`. History has ~24 h expiry
  (`Expires: Thu, 01 Oct 2026 11:05:00` for a 2026-09-30T11:05:03 `Last-Modified`).
  The Jun-2026 changelog records the pagination fix: "Market order fetching now correctly reads the
  `X-Pages` header to determine page count, preventing silent truncation of large regions" `[UPD]`.

**Do they filter to the hub station?** The docs say "regional endpoint" but call the prices
"in the selected trade hub" `[JS max_margin.js]`. Inference (strong): they query the hub's *region*
and then keep only NPC-station orders at the hub station. Evidence: on 2026-09-30 the site reported
Logic Circuit Buy 1,540,000 ISK, while the live `GET /markets/10000002/orders/` best buy across all
of The Forge was **1,686,000 ISK** — and that 1,686,000 order sits at `location_id 1044752365771`
(a player-owned structure) with `range: "solarsystem"`. The site's 1,540,000 is consistent with the
Jita 4-4 station (`location_id 60003760`) best buy of 1,539,000 at query time `[ESI-MKT]`.

**A caveat on the site's "structures are not included" claim.** CCP's ESI docs state: "Structure
market orders, **with the exception of ranged orders**, are not included in the results of
`/markets/{region_id}/orders/`. They have to be queried individually and then merged."
`[ESI-DOCS-STRUCT]`. Live data confirms this — The Forge regional feed contains buy orders at
`location_id 1044752365771` and `1042508032148` (structure-range IDs, system `30000144` =
Perimeter) with `range` values `1`, `5`, `solarsystem`, `region` `[ESI-MKT]`. Ranged structure
orders *do* appear in the public regional endpoint, so "citadel orders are not included" is only
true for non-ranged ones — unless, as inferred above, the site filters them by location, which the
one available data point suggests it does.

---

## 6. Where the site contradicts itself or CCP (verified)

### 6.1 The Max Margin "not profitable at max skills → excluded" gate does hold
`[UPD]` claims a max-skill net-margin floor. Verified against `[API]`: with
`min_margin=0&max_margin=6` the endpoint returns `{"data":[]}`; with `max_margin=7` it returns 100
rows whose lowest gross margin is **6.344 %**. The max-skill break-even gross margin (defined as
`(P_s−P_b)/P_s`) is **6.281 %** (`π = 0.95125·P_s − 1.015·P_b = 0`). So a ~6.3 % gross floor is
in force, consistent with "if a trade isn't profitable at max skills, it won't appear". ✓

### 6.2 Net margin: `/C` in the docs vs `/S` in the code (see §2.4) — **doc bug**
### 6.3 EDP: documented ≠ stored ≠ displayed (see §2.5) — **doc/code mismatch**
### 6.4 `Weighted Score` in Price Movement is natural log, not log10
`[REF]` says `Weighted Score = log10(V_d + 1)`. The JSON returns `"weighted_score": 22.46539` for
Tritanium at `V_d = 5,709,446,000`; `log10(5.709446e9) = 9.757` but `ln(5.709446e9) = 22.465`.
Same for Pyerite: `ln(2.66909e9) = 21.705` = the returned `21.705` `[API]`. The implementation
uses the natural log.

### 6.5 Broker Relations "can be further influenced by standings" — ignored (see §4.1)
### 6.6 Broker-fee base for a sell order is the *order value*, but the site charges it on `P_s` per unit
CCP: the broker fee is "based on a percentage of the **total order value**" `[CCP-FEES]`. The site's
per-unit model (`P_s × R_b`) is dimensionally consistent (it scales with the full order value once
multiplied by units), but it cannot represent the 100 ISK minimum or partial-fill relists.

### 6.7 The 80 % "scam" cap in Trading On a Budget does not appear in its output
`[REF]` and `[UPD]` both state "Max Margin = 80%: Excludes extremely high margins that likely
indicate scams or data errors." But `POST /trading_on_a_budget_page` with
`budget=100000000&min_margin=50&risk_tolerance=moderate` returns rows with `margin_raw` 99.91,
93.85, 93.88 … and names like "Compact Thermal Shield Amplifier" (buy 5,903 ISK, sell 6,922,000 ISK
in the same hub). The cap is either applied to a different metric or not enforced; the sources do
not say which. Either way the documented behaviour is not what the endpoint returns.

### 6.8 The hauling lanes have no sanity guard
`POST /arbitrage_lane_detail` for `Jita (The Forge) -> Hek (Metropolis)` returns a row for
"Tranquil Exotic Filament" with `source_price 1.04`, `dest_price 20030.0` and `net_margin_pct
**1860860.34**`, and "Negotiation" with `net_margin_pct 245.01`. The 5 %-of-best thin-book filter
and the 30-day price-band filter documented for Max Margin `[REF]` are evidently not applied to
arbitrage. Note that the *formula* is still internally consistent: 20,030 × 0.96625 − 1.04 =
19,352.9 (per unit); the site reports `net_profit 116,117.685` for 6 units = 19,352.9475 each ✓.

### 6.9 Anomaly severity/metrics are unexplained
Bucket names and severity labels exist, but no thresholds, z-scores, window lengths, or severity
cut-offs are published on the site (`[REF]` has no anomaly section). Only the developer's informal
statement of the ±20 % 7-day price rule and the 1.5× spread rule exists `[FAQ-2025]`. The JSON
`Anomaly Metric` strings are of the form `"7.2x Normal"`, with `Severity` ∈ High/Med/Low and
`Order Depth` of the form `"10 Buy / 178 Sell"` `[API]`. **Unclear from the sources.**

---

## 7. Summary answer for our engine

* **Fee model to copy verbatim:** `3% − 0.3%·BR` broker, `7.5%·(1 − 0.11·Accounting)` sales tax,
  broker charged on both legs, tax on the sale. Both constants agree with CCP `[CCP-FEES]`,
  `[ESI-TYPES]`.
* **Net margin denominator:** use `π / P_s` (revenue), not `π / C` — the References page's `/C`
  passage is contradicted by the site's own JS and its own changelog `[JS]`, `[UPD]`.
* **What eveprofits does *not* give us, and we must build ourselves:** undercut/overbid tick
  mechanics, relist fees, standings-adjusted broker fees, structure markets, escrow/Margin Trading,
  order-count skills beyond a hardcoded 150, and any documented anomaly scoring.
* **Data path precedent:** public ESI `/markets/{region_id}/orders/` with `X-Pages` pagination,
  refreshed on a ~30-minute cycle, plus daily `/markets/{region_id}/history/`; hub-only filtering
  by `location_id` (inferred) is what makes the numbers station-specific.

---

## Appendix A — raw probe evidence (2026-09-30, UTC)

Max Margin, Jita, `min_daily_volume=20`:

```
POST /max_margin_page
trade_hub=30000142&min_daily_volume=20&min_margin=10&max_margin=60&limit=5
->
{"data":[
 {"Avg Daily Volume":"70,081","Buy Price":"1,540,000.00 ISK",
  "Estimated Daily Profit":"3,195,689,040 ISK","Item Description":"Logic Circuit",
  "Margin (%)":"12.90%","Sell Price":"1,768,000.00 ISK"}, ... ]}
```

Hauling lane detail (`cargo_m3=12000&min_net_margin=5&min_profit_per_item=0`):

```
"summary": {"capacity_used_pct":99.99992916666666,"item_count":26,
 "roi":6.88383937326187,"total_investment":176522625.25,
 "total_net_profit":12151533.979675,"used_m3":11999.991499999998}
{"dest_hub":"Hek (Metropolis)","dest_price":1478000.0,"net_margin_pct":19.00979,
 "net_profit":228117.5,"profit_per_m3":2281175.0,"source_hub":"Jita (The Forge)",
 "source_price":1200000.0,"typename":"Clone Soldier Trainer Tag","units_to_buy":1}
```

Trading On a Budget order cap (`budget=1000000000000`):

```
{"success": true, "summary": {"budget_utilized_pct": 1.2122417481,
 "hit_order_limit": True, "max_orders": 150, "num_items": 150, ...}}
```

CCP ESI headers, `GET /markets/10000002/orders/?order_type=all&page=1` (2026-09-30 16:00 UTC):

```
cache-control: public
expires: Wed, 30 Sep 2026 16:01:57 GMT
last-modified: Wed, 30 Sep 2026 15:56:57 GMT
x-pages: 405
x-ratelimit-group: market-order
x-ratelimit-limit: 12000/15m
```

CCP-served skill text, `GET /universe/types/16622/` (Accounting):

> "Proficiency at squaring away the odds and ends of business transactions, keeping the checkbooks
> tight. Each level of skill reduces sales tax by 11%. **Sales tax starts at 7.5%.**"

`GET /universe/types/3446/` (Broker Relations):

> "Proficiency at driving down market-related costs. Each level of skill subtracts a flat 0.3% from
> the costs associated with setting up a market order in a non-player station, which usually come
> to 3% of the order's total value. **This can be further influenced by the player's standing
> towards the owner of the station where the order is entered.**"

## Appendix B — unresolved questions (for the caller)

1. Whether the site filters orders by hub `location_id` is an inference from a single-item price
   comparison, not a documented behaviour. Confirm by repeating the Logic Circuit-style comparison
   across several illiquid items, or by watching one item across two 30-minute cycles.
2. The exact server-side selection/ranking score for Max Margin ("balanced ranking (volume +
   margin)") is never defined; the JSON exposes no score field `[API]`, only the gross-based EDP.
3. The anomaly detector's features, windows and severity cut-offs are unpublished; `[FAQ-2025]`
   names an ML approach and two informal thresholds only.
4. Whether "150" corresponds to any concrete skill build (e.g. Trade V + Retail V + Wholesale V =
   base 5 + 20 + 40 + 80 = 145, so 150 is not that either). **Unclear from the sources.**
