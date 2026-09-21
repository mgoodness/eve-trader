# Prior art: P&L accounting for EVE Online station trading — relist/broker fees and cost basis

Research date: **2026-09-21**. Scope: how concrete EVE market/P&L tools account
for profit and loss on station trading, with focus on (a) broker fees across
re-lists and (b) the cost-basis method used. Sources are tool source code, tool
front-end JS, CCP dev blogs/patch notes, the ESI OpenAPI spec, ESI's issue
tracker, and the EVE University Wiki. Secondary/opinion material is labelled.

## Sources inspected (primary unless noted)

| Source | Version / commit | What it gave |
|---|---|---|
| `streetwraith/helion` | `7ee28be2` (2026-09-19) | Django market tool; FIFO realized P&L, pro-rata fee attribution, cost-basis notes |
| `GoldenGnu/jeveassets` | `b3af1d50` (2026-09-14) | Asset manager; per-order broker-fee/tax heuristic, valuation basis settings |
| `krojew/evernus` | `91d507ab` (2019-08-20, archived) | Qt market tool; fee amortization into unit prices |
| `marcfeather/EveTradeMaster` | `a2411b86` (2016-01-28) | PHP P&L tracker; transaction-pair matching, fee-in-price |
| `dpleshakov/EveOrderBook` | `05c60a7a` (2024-06-02) | Console margin calculator; min-100-ISK broker fee |
| `PeanutMotor/eve-motor-market` | `07da19d7` (2026-09-19) | German-language trader; FIFO cost basis, relist-fee amortization, real journal fees |
| `OrbitalEnterprises/eve-market-strategies` | `3922c4c6` (2020-10-01) | Book on EVE market making; fee/return formulas, simulator fee columns |
| eveprofits.com | retrieved 2026-09-21 | `static/js/net_margin_calculator.js`, `max_margin.js` |
| EVE University Wiki "Trading" | retrieved 2026-09-21 | Broker/sales/relist fee formulas, mechanics |
| `esi/esi-issues#1369` | open, updated 2026-06-29 | The missing `context_id` on fee/tax journal rows |
| ESI OpenAPI spec (`/meta/openapi.json`) | retrieved 2026-09-21 | Order/history fields and retention |
| CCP dev blogs (2019, 2020, 2021) + Patch 22.02 | retrieved 2026-09-21 | Fee/relist history |

Dead ends / not applicable: **Ravworks** is an industry production planner
("Profit Margins" for manufacturing, not station trading; `ravworks.com`,
retrieved 2026-09-21). **EVE Trade** (`evetrade.space`, retrieved 2026-09-21)
is a hauling/arbitrage route scanner, not a P&L/SKU-accounting tool. The EVE
Online support articles (`support.eveonline.com/.../203218962`, `.../203218932`)
returned Cloudflare HTTP 403 and could not be read. `EnetBan/EVE_Tycoon` is an
empty repo (only `.iml` + `requirements.txt`).

---

## Decision-relevant summary

1. **FIFO is the most common cost basis where a real lot is matched** (helion,
   eve-motor-market), but there is **no single convention**: weighted/average
   is common for per-item realized views and for valuation, and some tools use
   explicit buy-sell transaction pairing. Several "P&L" tools do not match lots
   at all — they amortize fees into a per-unit margin.
2. **Relist broker fees are almost never attributed to a specific order via an
   ESI join key**, because none exists. Tools either (a) amortize an *assumed*
   fee into per-unit cost, (b) report the *actual* journal fee as a separate
   aggregate expense, and/or (c) attempt a heuristic per-order match by
   timestamp/amount and warn it is incomplete.
3. **Realized and unrealized P&L are presented in separate views** in every
   tool that has both, and the realized view is where FIFO lives.
4. **Valuation of held/listed items** ranges from FIFO cost, to the trader's own
   past transaction prices (average/last/max/min), to current market net of
   fees; a few tools show both cost and net realizable value side by side.
5. Recurring pitfalls: both-sides vs one-side broker fee, single-charge vs
   relist-aware fee modeling, the 100 ISK minimum, stale fee formulas in older
   tools, cross-character lot pooling, ESI's short retention, and the ambiguity
   of "relist" (in-place price modify vs cancel + recreate).

---

## 1. Cost-basis methods in use, and why

### FIFO (oldest lot first) — used by the tools that track real positions
- **helion**, `market/services/flipping.py:3,67`: *"A sell consumes the oldest
  unmatched buy of the same item"*; `_match` pops the oldest lot from a
  `deque` per `type_id`. The FIFO rule is tested in
  `tests/test_flipping.py::TestMatching.test_the_oldest_lot_goes_first`.
- **eve-motor-market**, `eve_trader/market.py:26`: *"FIFO lot tracking PER
  CHARACTER (each character's sells consume only that character's own buy
  lots), then merged per type_id."* `realized_trades` (line 69) matches each
  sell against FIFO lots and computes `buy_avg` from the matched lots. A
  user-configurable "trading pair" (`paar`) can merge two characters' lots.

Why FIFO: it is the convention that produces an auditable lot with a real
purchase price when ESI gives you only per-transaction buys and sells. Neither
source states a regulatory/philosophical reason; both simply describe the
mechanics. (Observation, not sourced claim: FIFO also matches how a
market-maker mentally consumes inventory.)

### Weighted / moving average
- **helion**, `market/services/station_trading.py:_realized_profit` (line 294):
  per item, `volume = min(sell_history.volume, buy_history.volume)`, then
  `volume * sell_avg_price - volume * buy_avg_price`. This is a **min-volume
  average-cost** realized figure over all stored personal transactions — not
  FIFO.
- **helion**, `market/services/haul_tracker.py:_buys_on` + `_row`: cost of sold
  units = `units_sold * avg_buy_price`, where `avg_buy_price` is the
  volume-weighted average of that day's buys.
- **jeveassets** does not match lots; its "Transaction Profit" uses the
  trader's own transaction price distribution, configurable to
  **average / latest / maximum / minimum**
  (`ProfileData.java:1367-1404`, `Settings.getTransactionProfitPrice()`).

### Specific-lot / transaction pairing
- **EveTradeMaster**, `pages/scripts/internal/exec_update.php:960-1035`:
  iterates buys and sells in ascending time and pairs a buy against the
  earliest later sell of the same `item_eve_iditem`, storing one `profit` row
  per pair with `min(quantity_buy, quantity_sell)`. This is an explicit
  buy↔sell transaction match rather than a recomputed FIFO queue, though the
  ordering makes it effectively FIFO-ish.
- **EveTradeMaster** user-facing text (`pages/profit.php:147-150`): *"This view
  detects all item resales... Broker fees are always assumed regardless if you
  bought an item from a buy order or a sell order... Broker fees and transaction
  taxes are already included in prices."*

### Amortized per-unit cost with no lot identity (margin calculators)
- **eveprofits.com** `net_margin_calculator.js` (retrieved 2026-09-21):
  ```js
  const brokerRate = 0.03 - (0.003 * brokerRelationsLevel);
  const salesTaxRate = 0.075 * (1 - 0.11 * accountingLevel);
  const buyBrokerFee = buyPrice * brokerRate;
  const sellBrokerFee = sellPrice * brokerRate;
  const salesTax = sellPrice * salesTaxRate;
  const costPerUnit = buyPrice + buyBrokerFee + sellBrokerFee + salesTax;
  const profitPerUnit = sellPrice - costPerUnit;
  const netMargin = (profitPerUnit / sellPrice) * 100;
  ```
  It charges a broker fee on **both** the buy and the sell, once each, and has
  no relist term. Comment in-file: *"100 ISK minimum only applies to total
  order, not per unit."*
- **Evernus**, `PriceUtils.cpp:29-51`:
  `salesTax = 0.02 - Accounting*0.002`;
  `brokersFee = 0.03 - (BrokerRelations*0.001 + 0.0003*faction + 0.0002*corp)`;
  `getBuyPrice = limitOrder ? buyPrice + buyPrice*buyBrokerFee : buyPrice`;
  `getSellPrice = limitOrder ? sellPrice*(1-salesTax) - sellPrice*sellBrokerFee : ...`.
  Fees are folded into the unit prices. (Commit is 2019-08; the 0.001/level and
  2% sales-tax constants predate the July-2019 tax changes — see §5.)
- **EveOrderBook**, `ProfitMarginCalculator.cs`: adds `buyPrice*qty*buyBrokerFee`
  to total buy and subtracts `sellBrokerFee` and tax from total sell, enforcing
  `if (feeAmount < 100m) feeAmount = 100m` on both sides.

No source examined used LIFO or HIFO.

---

## 2. Broker fees on re-lists

### 2.1 What the game actually charges (primary CCP / EVE Uni)
EVE University Wiki "Trading" (retrieved 2026-09-21) states three charges:
**sales tax**, **broker's fee**, and **relist fee**.

- Sales tax: base **7.5%**, reduced **11% per level** of Accounting →
  **3.37%** at V (EVE Uni; footnote cites Patch 22.02, 2025-03-12:
  *"Sales Tax has been increased from 4% to 7.5%"*).
- Broker's fee (NPC station):
  `% = 3% − 0.3% × BrokerRelations − 0.03% × factionStanding − 0.02% × corpStanding`,
  **minimum 100 ISK**, paid when an order is created and not refunded.
- Relist fee (when an order is modified):
  `relist_fee = (100% − (50% + 6% × AdvancedBrokerRelations)) × BrokerFee% × NewOrderValue
              + BrokerFee% × max(NewOrderValue − OldOrderValue, 0)`,
  **minimum 100 ISK**. EVE Uni notes: *"In some cases it can cost more to modify
  an old order than it costs to take it down and set up a new order."*
- CCP dev blogs confirm the history: *"Updates to Sales Taxes & Brokers Fees"*
  (2019-07-29) raised max sales tax and the per-level reductions;
  *"Broker Relations"* (2020-02-24) introduced **"a new additional component,
  the Relist Charge"** and renamed Margin Trading → Advanced Broker Relations;
  *"Restructuring Taxes After Relief"* (2021-10-19) set base sales tax 8.0% /
  broker 3.0% (later reduced again to 7.5% sales tax by Patch 22.02).

### 2.2 Treatment A — amortize an assumed broker fee into per-unit cost
This is the dominant approach for scanners and older tools; it models **one
placement per unit** and does not distinguish a relist:
- eveprofits: broker on buy + sell, added to `costPerUnit` (above).
- Evernus: `getBuyPrice`/`getSellPrice` (above).
- EveOrderBook: `GetTotalBuy`/`GetTotalSell` (above).
- EveTradeMaster: `price_unit_b_taxed = price_unit_b * brokerFeeFrom * transTaxFrom`
  and `price_unit_s_taxed = price_unit_s * brokerFeeTo * transTaxTo`, then
  `profit = (sell_taxed − buy_taxed) × min(qty)`
  (`exec_update.php:1014-1025`). It *always* charges a broker fee on both sides
  whether or not the fill came from the trader's own limit order.

### 2.3 Treatment B — separate aggregate expense (actual journal fees)
- **helion**, `market/services/flipping.py:_totals`: the window's flip share of
  the real scheme-wide fee:
  ```python
  gross = sum(sale gross for every sale in window)      # matched or not
  fee  = brokers_fee_in_window * sell / gross            # sell = matched value
  profit = sell - cost + tax + fee                       # fee/tax are negative
  ```
  The fee row is labelled **"fees (approx.)"**. The module docstring:
  *"a broker fee names no item, so the flip share of the traded value is the
  closest thing to an attribution the data allows."*
- **helion**, `market/services/haul_tracker.py:185-189`:
  `brokers_fee = revenue * broker_rate`, with the comment *"Modelled, not
  measured: no `brokers_fee` row carries a context id, and the fee is charged
  when the order is placed... It understates, because a relisted order pays
  again and this charges once."*
- **eve-motor-market** profits tab (`ui/main_window.py:23366-23387`): it sums
  the **real** `brokers_fee` + `transaction_tax` journal entries for the window
  and reports `net_real = gross − fee_total`, with a tooltip: *"Actually paid
  according to the wallet journal... Broker (buy + sell + order changes)... Exact
  net profit (gross − real fees)"*, and *"ESI delivers the journal ~30 days
  back; the DB collects from introduction on – older periods are incomplete."*

### 2.4 Treatment C — attempt per-order attribution despite no join key
ESI does not expose an order link on fee rows. `esi/esi-issues#1369`
("Add context_id to transaction_tax & brokers_fee type journal entries") is
**open** (created 2023-11-29, updated 2026-06-29). Its own use case:
*"I pay additional brokers fee when I adjust my order, which makes profit
calculations very tricky."* The proposed example adds
`context_id_type: "market_transaction_id"`.

Tools work around this heuristically:
- **jeveassets**, `ProfileData.java:1279-1340`: groups `brokers_fee` journal rows
  and order-change events **by identical timestamp**; for each order it computes
  an `expected = max(volumeTotal * price / 100 * 5, 100)` (5% or 100 ISK upper
  bound) and greedily nearest-matches fee amounts to orders by
  `abs(fee + expected)`, summing matched fees per `order_id`. Sales tax is
  attributed the same way to sell transactions
  (`expected = qty*price/100*5`; `ProfileData.java:1264-1295`).
  The UI tooltip is explicit: *"Broker's Fee payed so far (May not include
  everything due to ESI limitations)"* (`i18n/TabsOrders.properties:23`), and
  the edit-count column carries the same caveat (`:25`).
- **eve-motor-market**, `store.py:bump_order_mod_count` / `get_buy_mod_fee_raw_by_type`:
  it **counts each in-place order modification per `order_id`** while the user
  works the order list, storing `mod_count`, `last_price`, `last_vol_remain`,
  then estimates `sum(mod_count * last_price * last_vol_remain) * currentBrokerRate`
  for **buy orders**. Comment: *"jede Nachbesserung kostet Broker-Gebühr...
  Sell-Order-Nachbesserungen sind ein Kostenpunkt beim Verkauf, nicht beim
  Einstand."* The estimate is then **amortized into the per-unit FIFO cost**
  (`ui/main_window.py:2396-2399`):
  `_est_fee_total = _fee_raw * broker_now`, `adj_avg = avg_buy + _est_fee_total / quantity`.
  UI note: *"Näherung – die genaue Zuordnung 'welche Fills gehören zu welcher
  nachgebesserten Order' ist über ESI nicht sauber möglich."* Note this estimate
  ignores the relist-discount term and the (new−old value) term of the official
  formula.
- **helion** deliberately does **not** attribute: `station_trading.py:_recent_counts`
  observes that *"ESI moves `issued` when a price changes, so a repriced order
  counts again"* — used to count competitor repricings, not fees. This
  "`issued` updates on reprice" behaviour is **attested by tool source, not by
  the ESI spec** (the spec only says `issued` = *"Date and time when this order
  was issued"*, OpenAPI `CharactersCharacterIdOrdersGet`). eve-motor-market
  likewise notes the `order_id` *"bleibt bei 'Order ändern' ingame gleich – nur
  bei Löschen+Neuanlegen ändert sie sich"*. So a "relist" can be an in-place
  price modify (same `order_id`, `issued` moves) or a cancel+recreate (new
  `order_id`), and only the latter leaves a `cancelled` row in order history.
- Related ESI gaps that make exact attribution worse: `esi/esi-issues#612`
  (closed 2018) confirms **fully-filled orders never appear** in either orders
  route, and `GET /characters/{id}/orders/history` returns only
  `state: ["cancelled","expired"]` (OpenAPI spec) with 90-day retention.

**Answer to the direct question:** per-order fee attribution *is* attempted by
at least jEveAssets (date + amount nearest-match) and eve-motor-market
(manual mod-count per surviving `order_id`), and both label it incomplete. The
only tools that avoid the problem are those that treat fees as (a) a per-unit
formula assumption or (b) an aggregate expense.

---

## 3. Realized vs unrealized P&L presentation

- **helion**: separate concerns. The index **"flipping"** table is realized only
  — matched buy/sell unit pairs, *"Stock that is still unsold carries no sell
  yet, so it waits"* (`flipping.py:1-9`). The trade-hub **desk** shows a
  per-item `my_profit` realized over `min(volume)` (`station_trading.py`), and
  holdings/valuation are shown separately via assets and net values.
- **eve-motor-market**: a **"Gewinne" (profits) tab** = realized FIFO sell
  events (`market.realized_trades`, net/gross/revenue/margin, windowed), and a
  separate **Portfolio tab** = unrealized (`invested = Σ avg_buy×qty`,
  `value = Σ net_unit×qty`, `pl = value − invested`; `ui/main_window.py:2262-2276`).
  It also shows wallet + open sell orders + buy escrow as total wealth.
- **jeveassets**: per-order columns rather than a P&L statement:
  `TRANSACTION_PROFIT` ("compared to transactions (does not include tax)"),
  `TRANSACTION_PROFIT_PERCENT`, `MARKET_PROFIT` ("compared to market price
  (does not include tax)"), and `BROKERS_FEE`
  (`gui/tabs/orders/MarketTableFormat.java:170-521`;
  `i18n/TabsOrders.properties:53-66`).
- **EveTradeMaster**: realized only — a stored `profit` row per matched
  buy/sell pair, aggregated per day/character.
- **eve-market-strategies** (book, primary-authored simulation): fees are kept
  as **separate columns** `gross`, `tax`, `broker_fee`; the text says the
  DataFrame *"does not compute running PNL. Instead, we must add the
  appropriate columns manually"* and `(gross − tax − broker_fee).sum()`
  (chapter 03).

---

## 4. Conventions for valuing items held/listed

| Convention | Where observed |
|---|---|
| **FIFO cost basis** for held inventory | eve-motor-market `holdings_from_assets` prices `/assets` quantities with `avg_buy` from `aggregate_holdings` |
| **Weighted average of own buys** | helion `get_average_transaction_price_bulk` (volume-weighted, 90-day default) |
| **Last own transaction price** | helion `get_trade_history_bulk` (`last_price` per type) |
| **Own-transaction price statistic, user-configurable** (average / latest / maximum / minimum) | jeveassets `setLastTransaction` / `getTransactionAveragePrice` |
| **Current market price** | jeveassets `getMarketProfit` = `dynamicPrice − orderPrice`; helion trade-hub cells |
| **Current market net of tax+fee** (realizable) | eve-motor-market `evaluate`: `instant → market*(1−tax)`; `limit → market*(1−tax−broker)` |
| **Amortized cost incl. fees** (not a pure cost basis) | eveprofits `costPerUnit`; Evernus `getBuyPrice`; EveOrderBook `GetTotalBuy` |

Notably, most tools present held items at **cost** in one column and at
**market/net-realizable** in another, rather than asserting one "true" value:
jeveassets has both Transaction Profit (vs own trades) and Market Profit (vs
market); eve-motor-market shows `avg_buy` (FIFO) and `net_unit` (market) in
adjacent columns and exposes a separate `adj_avg` = FIFO cost + amortized relist
fees.

---

## 5. Pitfalls, disagreements, and unresolved questions

1. **Is the broker fee charged on one side or both?** Disagreement.
   eveprofits, Evernus (limit orders), EveOrderBook, and EveTradeMaster charge
   broker on both buy and sell. helion's haul tracker charges one sell-side
   `revenue * broker_rate` (explicitly *"understates... a relisted order pays
   again"*). For a station trader placing both a buy order and a sell order,
   two charges are the realistic baseline; for an instant market buy, a
   buy-side broker fee does not occur. eve-motor-market's journal tooltip
   enumerates *"Broker (buy + sell + order changes)"*.
2. **Single placement vs relist-aware.** eveprofits/Evernus/EveOrderBook models
   have no relist term at all. helion charges once and says so. eve-motor-market
   is the only tool found that both **counts relists** and **amortizes their
   estimated fee into cost**, and it labels the estimate approximate. The
   *official* relist formula (EVE Uni, with the Advanced Broker Relations
   discount and the `max(new−old, 0)` term) was **not** found implemented
   verbatim in any examined tool.
3. **No fee→order join key.** `esi/esi-issues#1369` is open; both jEveAssets and
   eve-motor-market resort to heuristics/manual counting and warn about
   completeness. helion declares exact attribution impossible from the data.
4. **The 100 ISK minimum.** EveOrderBook enforces it on both sides; eveprofits
   explicitly notes it applies to the total order, not per unit. helion's
   `fees.py` and EVE Uni state the minimum exists. Older tools (EveTradeMaster)
   had it only in commented-out code (`exec_update.php:1022-1023`).
5. **Stale fee formulas.** Evernus (2019) uses `0.03 − 0.001×BR` and
   `0.02 − 0.002×Accounting`; EveTradeMaster (2016) uses base broker `0.01` and
   sales tax `(1.5 − 0.1×1.5×Accounting)%`. These do not match current live
   rates (EVE Uni / Patch 22.02: base broker 3%, base sales tax 7.5%). helion's
   `fees.py` uses `0.03 − 0.003×BR − 0.0003×faction − 0.0002×corp` and
   `0.0337`, and hard-codes a `SALE_PROCEEDS_PERCENT = 96.4` that it admits is
   inconsistent with its own functions (which give 95.62).
6. **Sales-tax attribution.** helion claims an *exact* split, because a
   `transaction_tax` row shares its exact second with the sales it was charged
   on and one character/rate applies per second (`flipping.py:_matched_tax`,
   `haul_tracker.py:_sales_tax_by_type`). jeveassets instead nearest-matches by
   a 5% expectation, which is approximate. This is a real methodological
   disagreement; helion's second-based claim rests on the wallet journal's
   timestamp granularity.
7. **Cross-character lot matching.** eve-motor-market ships FIFO **per
   character** by default and only pools lots for an explicitly declared
   "trading pair"; it documents a real user case where 26.4 B ISK of trades
   vanished from the profits tab because the buy and sell were on different
   characters. helion pools lots across the set of characters flagged as
   traders (`flipping.py:_transactions`, `test_the_lots_pool_across_the_trader_characters`).
8. **ESI retention forces local persistence.** eve-motor-market's comments note
   ESI returns only ~2500 newest transactions per call and ~30 days of journal,
   so older buys — *"und mit ihnen die FIFO-Kostenbasis"* — are lost forever if
   not stored. helion and jeveassets likewise accumulate history locally. This
   is a correctness prerequisite for any FIFO realized-P&L feature.
9. **"Relist" is ambiguous.** In-place price modify keeps `order_id` and
   updates `issued` (tool-attested, not in the spec), while cancel+recreate
   produces a `cancelled` history row and a new `order_id`. Only the latter is
   visible as a cancel in `/orders/history`, and `esi/esi-issues#612` means
   fully-filled orders are absent from both routes. Exact relist-chain
   reconstruction is therefore not guaranteed.
10. **EVE University internal inconsistency (secondary source).** The same page
    says Accounting takes sales tax *"from 8% to 3.6%"* in the skills paragraph
    but *"base sales tax is 7.5%... 3.37% at max skills"* in the tax section.
    The footnote points at Patch 22.02 (7.5%), which is the newer figure and
    matches eveprofits (`0.075 × (1 − 0.11×level)` = 3.375%) and helion
    (`0.0337`). Treat 7.5%/3.37% as current.

### Unresolved / not found
- No examined tool reconstructs relist chains from `/orders/history`
  (`cancelled` → later order) for fee accounting; they either count in-place
  modifies or use aggregate journal sums.
- No CCP source was found that documents the `issued`-updates-on-reprice
  behaviour; it is attested only by tool source.
- The EVE Online help-center articles on broker fee / buy-and-sell orders were
  inaccessible (HTTP 403), so the exact official wording of the relist minimum
  and formula is cited here via the EVE University Wiki transcription and the
  2020 dev blog rather than the help center itself.
