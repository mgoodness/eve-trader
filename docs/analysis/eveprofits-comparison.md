# eve-trader vs. eveprofits.com `max_margin_page`

Status: **point-in-time analysis**, not a decision record. See
[`CONTEXT.md`](../../CONTEXT.md) for the vocabulary used here and
[`docs/spec/v1.md`](../spec/v1.md) for the ranking formula.

Captured 2026-09-21, against `main` at commit `440750a`. The comparison was
run with eveprofits set to **Rens**, **min daily volume 20**, **min gross
margin 8%**, **max gross margin 60%**, **no max sell price**, **1000
results**, and the default **Accounting 5 / Broker Relations 5** skills.

## Question

Why does eve-trader's opportunity table disagree with
<https://www.eveprofits.com/max_margin_page> when both are pointed at Rens
with a minimum gross margin of 8%?

## Method

Both sides were reproduced from live primary sources, not from screenshots:

1. **eveprofits.** POST to `https://www.eveprofits.com/max_margin_page` with
   the filter values above (`trade_hub=30002510` is the Rens *system* id).
   Captured the JSON response and the page's client scripts
   (`/static/js/max_margin.js`, `/static/js/net_margin_calculator.js`).
2. **eve-trader.** Pulled `GET /markets/10000030/orders/` from ESI (73 pages,
   ~72k orders), filtered to Rens station `location_id=60004588`, and fetched
   `GET /markets/10000030/history/` for all 3,350 candidates (both order-book
   sides present). Re-implemented `ranking.Load`'s query, realism filters,
   fee math, and sort on that data.
3. Diffed the two row sets (matching names to type ids via
   `POST /universe/ids/` and `POST /universe/names/`).

Our reconstruction produced **166 rows**; eveprofits returned **274**.
**162 were shared.**

## Findings

### 1. The single-order-spread realism filter is the dominant divergence

Our always-on realism filters hid **2,672 of 3,350 candidates**:

| Rule | Hidden |
|---|---:|
| single-order BUY side | 1,162 |
| single-order BOTH sides | 688 |
| single-order SELL side | 253 |
| thin history (<7 trade-days) | 370 |
| incomplete / no history | 100 |
| manipulated history | 99 |

The single-order rule alone accounts for **2,103 of 2,672 (79%)** of all
realism exclusions. Of eveprofits' 274 rows, **~101** are items we hide
because one side rests on a single order within 5% of its best price. The
clearest example is eveprofits' **#1 row, Large Skill Injector**:

- best buy `680,100,000` ISK for **2 units**, next buy `772,200` ISK
- our `buyNear = 1` → hidden; eveprofits shows it at a 10.4% gross margin

Other items we hide and eveprofits shows: `Navy Cap Booster 3200`,
`Tritanium`, `Compressed Plagioclase`, `Medium Trimark Armor Pump I`,
`Light Neutron Blaster II`, `Crystalline Kangite`, `Precious Metals`,
`Datacore - Hydromagnetic Physics`.

Our implementation matches the documented definition in
[`CONTEXT.md`](../../CONTEXT.md) ("one or fewer orders within 5% of that
side's best price") and the rule asserted in the page's own footnote. The
divergence is that **eveprofits advertises the filter but effectively does
not apply it**. Loosening tests did not reconcile the two: a 50% band still
hides 55/274, and even the loosest reading ("≤1 order total on a side")
hides 34/274.

### 2. ISK/day: we apply the 20% capture rate, the eveprofits page does not

- Ours: `EDP = π × V_d × 0.20` (`ranking.CaptureRate`).
- Theirs: `net_margin_calculator.js` computes
  `calculateEstimatedDailyProfit = profitPerUnit × avgDailyVolume` — **no
  0.20 factor**. Their footer text still claims "~20% of daily volume", and
  the server JSON emits `gross × volume × 0.2`, but the client overwrites the
  rendered cell, so the page is internally inconsistent.

For an identical row, **our ISK/day is ~5× smaller** than what the page
displays. Ranking is unchanged (constant factor). Separately, ours uses the
authenticated character's Broker Relations / Accounting while the page
defaults to 5/5, which shifts net margin and ISK/unit but not the gross-margin
filter.

### 3. eveprofits' max-margin filter runs on a margin that sometimes differs from its own displayed prices

For **20 of 274** rows, the JSON `Margin (%)` does **not** equal
`(sell − buy) / sell` of the displayed prices. Examples:

| Item | gross from displayed prices | their `Margin (%)` |
|---|---:|---:|
| Crystalline Kangite | 65.11% | 28.43% |
| Warp Scrambler II | 78.06% | 31.58% |
| Small Cargohold Optimization I | 86.57% | 36.67% |
| Medium EM Shield Reinforcer II | 60.82% | 26.00% |

Every one of the 20 is a high-gross item, and they are exactly the ones that
pass their `max_margin=60` but fail ours (which applies the cap to true gross).
The likely cause is that their server-side margin is computed from an older
buy price than the `Buy Price` they display (a new, very low buy order having
appeared since). The exact formula was not derived. Net effect: **11 items on
their list we hide as >60% gross.**

## What is *not* a divergence

Verified against live ESI data:

- **Price scope.** Both use the Rens **station** `60004588`, not the region.
  252/274 prices matched exactly; the 22 that did not were explained by cache
  timing (their ~10 min refresh vs. our ~5 min poll; several were orders
  `issued` minutes before the fetch). Region-scoping matched only 121/274.
- **Volume.** Both use the Heimatar **region** 30-day average. 138/274 matched
  within 2%; the rest track the once-daily ESI history update.
- **Thin / manipulated history, min volume 20, min gross margin 8.** No row in
  their 274 was dropped by these on our side, so the rules agree in practice.
- The **4 rows we show that they do not** (`Charred Micro Circuit`,
  `Fullerite-C320`, `Nanite Repair Paste`, `Scourge Cruise Missile`) sit right
  at the single-order boundary — most likely snapshot timing on their side.

## Reconciliation

| | rows |
|---|---:|
| eve-trader rows | 166 |
| eveprofits rows | 274 |
| shared | 162 |
| only eveprofits | 112 |
| only eve-trader | 4 |

The 112 only-eveprofits rows break down as roughly **101** hidden by our
single-order rule and **11** hidden by our >60% gross cap.

## Caveats

- Point-in-time: order books and the once-daily history window move. Re-running
  will shift the exact counts and the 20-row margin anomaly.
- Our reconstruction assumed max skills (5/5) to match the page's default; a
  character with lower skills changes net margin and ISK/unit, not the
  gross-margin row set.
- The analysis deliberately did not treat eveprofits' numbers as a target — its
  page has its own internal inconsistencies (capture rate, server vs. client
  EDP, margin field). The durable takeaways are the *structural* differences:
  the literal single-order rule, the capture multiplier, and the margin basis.
