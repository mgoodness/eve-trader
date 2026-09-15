# Research: station-trading profitability formula & ESI skill modifiers

**Ticket:** issue #3
**Date:** 2026-09-14
**Question:** What exact profitability formula(s) does eveprofits use for station trading, and which ESI-exposed character skills/standings modify broker fees and sales tax?

## TL;DR

- eveprofits' net-margin formula for station trading is **current and correct** for the game mechanics as of the 2025-03-12 (version 22.02) patch: base sales tax 7.5%, reduced 11%/level by **Accounting**; base broker fee 3%, reduced 0.3 percentage points/level by **Broker Relations**.
- **Discrepancy found:** eveprofits' broker-fee formula omits the **standings** terms that CCP's own formula still includes. eveprofits effectively assumes zero (neutral) faction/corp standing. For a character with positive standing toward the station-owning NPC corp/faction, eveprofits (and any port of its formula) will *overstate* the broker fee and therefore *understate* net profit. It's simplifying, not wrong — but it's a real gap if eve-trader wants numbers to be accurate for a specific character.
- Margin Trading (mentioned in the ticket as a maybe-relevant skill) was **removed from the game on 2020-03-10** and no longer exists — do not attempt to look it up via ESI.
- ESI skill type IDs needed: **Broker Relations = 3446**, **Accounting = 16622** (confirmed independently via ESI `/universe/types/{id}/` descriptions and via the Fuzzwork SDE mirror).
- Character's actual skill level: `GET /characters/{character_id}/skills/` (scope `esi-skills.read_skills.v1`), field `active_skill_level` per skill (not `trained_skill_level` — see caveat below).
- If a follow-up ticket wants to fold in standings too: `GET /characters/{character_id}/standings/` (scope `esi-characters.read_standings.v1`) returns `standing` per `from_type` (`npc_corp` / `faction`) / `from_id`, matched against the station-owning corp/faction for Rens.

---

## 1. eveprofits' formulas (from https://www.eveprofits.com/references, fetched 2026-09-14)

The page is titled "References and Formulas" and covers several of its analysis tools. The station-trading-relevant ones:

### Gross margin (used across multiple tools, e.g. "Max Margin Opportunities", "Price Movement")

```
M = (P_s - P_b) / P_s × 100%
```
- `M` = gross margin %
- `P_s` = sell price, `P_b` = buy price

### Net margin (station trading — "own buy + own sell order" model)

This is the formula that matches eve-trader's model (buying and selling via your own orders in the same station). Verbatim from the page's "Max Margin — Ranking & Realism Filters" section (re-fetched and confirmed 2026-09-14):

> "Max Margin opportunities are ranked by net estimated daily profit (post-fee), not gross spread. The net profit per unit is the gross spread minus the broker fee on both the buy order and the sell order, minus sales tax on the sale:
> $$\pi = P_s - P_b - (P_b \times R_b) - (P_s \times R_b) - (P_s \times R_t)$$
> The fee rates scale to your skills — broker fee $R_b = 3\% - (0.3\% \times \text{Broker Relations})$ and sales tax $R_t = 7.5\% \times (1 - 0.11 \times \text{Accounting})$, i.e. 1.5% and 3.375% at level V."

Rewritten with the notation used in the rest of this doc:

```
R_b = 3% − (0.3% × Broker Relations level)          # broker fee rate
R_t = 7.5% × (1 − 0.11 × Accounting level)           # sales tax rate

F_b,buy  = P_b × R_b        # broker fee to place the buy order
F_b,sell = P_s × R_b        # broker fee to place the sell order
F_t      = P_s × R_t        # sales tax, paid on the sell

C  = P_b + F_b,buy + F_b,sell + F_t     # total cost per unit
π  = P_s − C                             # net profit per unit (matches eveprofits' π exactly)
M_net = π / C × 100%                     # net margin, cost basis
```

An alternate net-margin expression used on their "Trading On a Budget" page expresses margin as % of *revenue* instead of cost:

```
M_net = (P_s − P_b − F_b − F_s − T_s) / P_s × 100%
```
(same `F_b`, `F_s`, `T_s` terms — just a different denominator convention; worth deciding which basis eve-trader wants.)

### Estimated daily profit

```
EDP = π × V_d
```
- `π` = net profit per unit (after all fees)
- `V_d` = average daily volume

eveprofits additionally applies a **20% capture-rate** assumption to `V_d` in its ranking (you don't capture 100% of daily volume because you compete with other traders) and several "realism filters" (30-day price-band clamp, thin-book exclusion, minimum 7-day history) before ranking by `EDP` — not formula per se, but worth knowing if eve-trader wants to replicate the ranking, not just the raw formula.

### Order-slot cost

eveprofits does **not** model order-slot cost as an ISK figure. It treats the 150-active-order limit (EVE Online's per-character cap, confirmed independent of skills at max trade skills) as a hard constraint in its knapsack-style "Trading On a Budget" tool, not as a fee.

### Minimum broker fee note (explicitly called out by eveprofits)

> "Broker fees have a 100 ISK minimum per order (not per unit). This primarily affects single-unit trades of low-value items and is not factored into the per-unit margin calculations shown on this site, as volume trading rarely encounters this minimum."

This matches CCP's actual minimum broker fee (see §2). eve-trader should decide whether to model this minimum explicitly for low-value items — eveprofits deliberately does not.

### Worked example from the page (max skills, 5/5)

- Broker Fee Rate at BR V: 1.5%
- Sales Tax Rate at Accounting V: 3.375% (7.5% × 0.45)
- "total fee burden of ~6.375%" (1.5% × 2 sides + 3.375%) at max skills

### Fill-existing-order model (their "Hauling Routes" tool — *not* eve-trader's model, included for completeness/contrast)

When you fill someone else's existing order rather than placing your own (i.e. immediate/market order, no broker fee), only sales tax applies on the sell side:

```
π = P_dest × (1 − t) − P_source
M_net = π / P_source × 100%
```

This is *not* the model eve-trader needs (v1 is "buy low, sell high within Rens's own order book," i.e. both sides are the trader's own limit orders), but it's useful to know eveprofits distinguishes the two cases explicitly, and that only the "own order book" model incurs broker fees at all.

---

## 2. Actual EVE Online game mechanics (current, as of 2026-09-14)

Primary sources: EVE University wiki "Trading" page (https://wiki.eveuniversity.org/Trading, reflects the March 2025 tax change), official CCP patch notes (https://www.eveonline.com/news/view/patch-notes-version-22-02), the CCP support article "Broker Fee and Sales Tax" (https://support.eveonline.com/hc/en-us/articles/203218962, archived copy via Wayback Machine since the live page 403s to non-browser requests), and the skill descriptions returned live from ESI itself (`/universe/types/{id}/`), which is as primary a source as it gets since it's the game's own data.

### Sales tax

- **Base rate: 7.5%** of the sale price, paid by the seller, deducted automatically from the transaction. (This was raised from 4% to 7.5% in patch **22.02**, released 2025-03-12 — confirmed directly in CCP's patch notes, which list `Sales Tax has been increased from 4% to 7.5%.` under Science & Industry.)
- Reduced by the **Accounting** skill: **11% (relative) per level**.
- Formula (EVE University, matches ESI's own skill description text — see below):

  ```
  SalesTax% = 7.5% × (1 − 0.11 × AccountingLevel)
  ```

  At Accounting V: `7.5% × (1 − 0.55) = 7.5% × 0.45 = 3.375%`.

- **ESI confirms this directly.** `GET /universe/types/16622/` (Accounting) returns description: *"Proficiency at squaring away the odds and ends of business transactions, keeping the checkbooks tight. Each level of skill reduces sales tax by 11%. Sales tax starts at 7.5%."* — this is CCP's own live game data, and it matches eveprofits' formula exactly. No skill/mechanic other than Accounting affects sales tax; sales tax is always paid to the NPC "Secure Commerce Commission," not affected by standings.

### Broker fee

- **Base rate: 3%** of the total order value, charged on **order creation** for any non-immediate order (i.e. both buy and sell limit orders), non-refundable if the order is later cancelled/expires. This has been 3% since the 2021-10-19 "Restructuring Taxes After Relief" patch (https://www.eveonline.com/news/view/restructuring-taxes-after-relief) and hasn't changed since (no later patch note updates the 3% NPC-station figure — the 2025-03 patch only touched sales tax).
- Reduced by the **Broker Relations** skill: **0.3 percentage points per level** (flat, not relative like Accounting).
- **Also reduced by NPC standings toward the station-owning corporation and faction** — this is the part eveprofits omits. From CCP's own support article (confirmed via Wayback Machine snapshot, 2023-12-10, and unchanged per the current EVE University wiki citation):

  > "Starting at 3% of the order value, the skill 'Broker Relations' reduces the fee by 0.3% per level. In addition, increased standings with the owner of the NPC station where the order is placed may reduce it by up to another 0.2% with maximum standing, and good standings with the owners faction can reduce the fee by further 0.3%, to a minimal broker fee of 1% of the order value."

  Exact formula (CCP support article + EVE University wiki, both cite the same numbers):

  ```
  BrokerFee% = 3% − (0.3% × BrokerRelationsLevel) − (0.03% × FactionStanding) − (0.02% × CorpStanding)
  ```

  - `FactionStanding` / `CorpStanding` are the character's **unmodified** (base) standing values (range roughly −10..+10) toward the faction and NPC corporation that own the station. Skills/effects that only *boost the effective/displayed* standing (e.g. Connections, Diplomacy) do **not** feed into this formula — only the raw standing value counts.
  - Corp standing contributes at 2/3 the rate of faction standing (0.02 vs 0.03 percentage points per standing point).
  - Floor: minimum broker fee is **1%** of order value even at max skills + max standings (BR V + 10/10 standing: `3% − 1.5% − 0.3% − 0.2% = 1.0%`).
  - Minimum absolute fee: **100 ISK** per order, regardless of order value (matches eveprofits' note).
  - **This entire skill/standing calculation does not apply in player-owned (Upwell) structures** — those charge a fixed 0.5% SCC surcharge plus a fee set by the structure owner, unaffected by Broker Relations or standings. Not relevant to eve-trader's Rens-NPC-station scope, but worth remembering if eve-trader ever expands beyond NPC stations.

  **ESI confirms the skill side directly.** `GET /universe/types/3446/` (Broker Relations) returns description: *"Proficiency at driving down market-related costs. Each level of skill subtracts a flat 0.3% from the costs associated with setting up a market order in a non-player station, which usually come to 3% of the order's total value. This can be further influenced by the player's standing towards the owner of the station where the order is entered."* — corroborates both the skill formula and the fact that standings are a real modifier CCP still applies today.

### Relist fee (not currently in eveprofits' formulas, flagging for completeness)

Changing the price of an existing order charges a separate "relist fee" (introduced March 2020), reduced by **Advanced Broker Relations**. eveprofits' reference page does not mention this at all — it isn't part of their margin math, and is out of scope for eve-trader v1 (which is presumably placing new orders, not repricing existing ones), but flag it if v1 ever adds order-repricing logic.

### Margin Trading — obsolete, not relevant

The ticket asked about Margin Trading as a "worth noting" skill. Per EVE University's Trade skills page: Margin Trading **doesn't exist anymore** — it "was the Margin Trading skill prior to 2020-03-10, which reduced the amount of ISK that had to be held in the market alongside a buy order before it was filled." The March 2020 rework removed the partial-collateral mechanic entirely (buy orders now require full ISK collateral up front, unconditionally), and the skill was retired. **Do not look for an ESI skill type ID for it** — it won't resolve to anything meaningful in current game data (confirmed: neither ESI's `/universe/ids/` nor the Fuzzwork SDE mirror resolve "Margin Trading" to an inventory-type ID anymore).

---

## 3. Cross-reference: does eveprofits match current mechanics?

| Component | eveprofits formula | Current CCP mechanic | Match? |
|---|---|---|---|
| Sales tax base | 7.5% | 7.5% (since 22.02, 2025-03-12) | ✅ Match |
| Sales tax skill reduction | 11%/level, Accounting | 11%/level, Accounting (confirmed via live ESI skill description) | ✅ Match |
| Broker fee base | 3% | 3% (since 2021-10-19) | ✅ Match |
| Broker fee skill reduction | 0.3 pp/level, Broker Relations | 0.3 pp/level, Broker Relations (confirmed via live ESI skill description) | ✅ Match |
| Broker fee standings reduction | **Not modeled** (assumes 0) | −0.03%×FactionStanding −0.02%×CorpStanding | ❌ **Gap** — eveprofits ignores standings entirely |
| Minimum broker fee (100 ISK/order) | Noted, not applied per-unit | Confirmed, 100 ISK/order | ✅ Match (both choose not to fold it into per-unit margin) |
| Order-slot / active-order cap | 150 orders, treated as hard constraint in budget tool | 150 is the max regardless of skills at max trade skills (Trade/Retail/Wholesale/Tycoon) — not itself a "fee" | ✅ Consistent framing |

**Conclusion:** eveprofits is using the *current* (post-2025-03-12) sales-tax rate and the *current* broker-fee skill formula — it is not stale/pre-rework math. The one real gap is that **it doesn't model standings' effect on broker fees at all**, i.e. it always computes the broker-fee rate as if the trader had neutral (0) standing with the station's owning corp and faction. Since standings only ever *reduce* the fee, eveprofits' broker-fee-inclusive net margin is a conservative *lower bound* on the true net margin for any character with non-negative standings — never an overestimate.

For eve-trader v1's stated goal ("real broker fees and sales tax accurate for that specific trader"), this is the concrete decision to make in the follow-up ticket: **do we replicate eveprofits' simplification (skills only, standings assumed neutral) or go one step further and pull the character's actual station-owner standings via ESI too?** Rens is in the Minmatar Republic (Brutor Tribe space); the specific NPC corporation and faction that own the Rens station would need to be looked up via the station/structure info endpoint to know which `from_id`/`from_type` pair in the standings response applies.

---

## 4. ESI attributes needed (for the follow-up implementation ticket)

### Skill type IDs (confirmed two independent ways: ESI `/universe/types/{id}/` and the Fuzzwork SDE mirror)

| Skill | Type ID | Effect |
|---|---|---|
| Broker Relations | **3446** | −0.3 percentage points off broker fee, per level |
| Accounting | **16622** | −11% (relative) off sales tax, per level |
| Advanced Broker Relations | 16597 | (not needed for margin calc — only affects relist fee) |
| Trade | 3443 | (not needed for margin calc — only affects order-slot count) |
| Margin Trading | — (removed 2020-03-10, no current type ID) | N/A, obsolete |

### Endpoint: character skills

```
GET /characters/{character_id}/skills/
Scope: esi-skills.read_skills.v1
```

Response shape (from ESI's own OpenAPI spec, `CharactersSkills` / `CharactersSkillsSkill` schemas):

```json
{
  "skills": [
    {
      "skill_id": 3446,
      "trained_skill_level": 4,
      "active_skill_level": 4,
      "skillpoints_in_skill": 90000
    }
  ],
  "total_sp": 5000000,
  "unallocated_sp": 0
}
```

To get a character's current Broker Relations / Accounting level: filter `skills[]` where `skill_id == 3446` / `skill_id == 16622` and read **`active_skill_level`** — not `trained_skill_level`. ESI's own doc note: *"active_skill_level ... can differ from trained due to alpha status and/or active expert systems"*. `active_skill_level` is the level that actually applies in-game (e.g. an Alpha-clone character's effective level is capped even if `trained_skill_level` is higher), so it's the correct field for computing real fees. If a skill ID isn't present in the array at all, treat it as level 0 (untrained).

### Endpoint: character standings (only needed if the follow-up ticket decides to model standings too — see §3 decision point)

```
GET /characters/{character_id}/standings/
Scope: esi-characters.read_standings.v1
```

Response shape (`CharactersCharacterIdStandingsGet` schema):

```json
[
  { "from_id": 1000035, "from_type": "npc_corp", "standing": 2.5 },
  { "from_id": 500002,  "from_type": "faction",   "standing": 1.0 }
]
```

`from_type` is one of `agent` / `npc_corp` / `faction`. To compute the broker-fee standings terms for Rens specifically, eve-trader would need to know the `corporation_id` and `faction_id` that own the Rens station (via the station/structure lookup endpoints), then match those against `from_id` in this array to get `CorpStanding` and `FactionStanding`.

---

## 5. Worked numeric example (adapted to eve-trader's own-order-book model)

Character with **Broker Relations IV** and **Accounting III**, assuming neutral (0) standings (matching eveprofits' simplification), trading a unit with buy price 100,000 ISK and sell price 110,000 ISK:

```
R_b = 3% − (0.3% × 4) = 3% − 1.2% = 1.8%
R_t = 7.5% × (1 − 0.11 × 3) = 7.5% × 0.67 = 5.025%

F_b,buy  = 100,000 × 1.8%   = 1,800.00 ISK
F_b,sell = 110,000 × 1.8%   = 1,980.00 ISK
F_t      = 110,000 × 5.025% = 5,527.50 ISK

C  = 100,000 + 1,800 + 1,980 + 5,527.50 = 109,307.50 ISK
π  = 110,000 − 109,307.50 = 692.50 ISK per unit
M_net (cost basis)    = 692.50 / 109,307.50 × 100% ≈ 0.634%
M_net (revenue basis) = 692.50 / 110,000 × 100%   ≈ 0.630%

Gross margin (for comparison) = (110,000 − 100,000) / 110,000 × 100% ≈ 9.09%
```

This shows how much fees eat into a nominally-9% gross margin — down to well under 1% net at these mid-tier skill levels — which is exactly why eve-trader needs the real per-character skill levels rather than assuming max skills: at max skills (BR V, Accounting V) the same trade nets `R_b = 1.5%`, `R_t = 3.375%`, giving `F_b,buy=1,500`, `F_b,sell=1,650`, `F_t=3,712.50`, `C=106,862.50`, `π=3,137.50`, `M_net≈2.94%` — nearly 5x the net profit of the level-4/level-3 example above.

---

## Sources

- eveprofits references page: https://www.eveprofits.com/references (fetched 2026-09-14)
- eveprofits dashboard (context only): https://www.eveprofits.com/dashboard
- EVE University wiki, "Trading": https://wiki.eveuniversity.org/Trading (reflects patch 22.02, current as of fetch)
- EVE University wiki, "Skills:Trade" (Margin Trading obsolescence note): https://wiki.eveuniversity.org/Margin_Trading (redirects to Skills:Trade)
- CCP official patch notes, Version 22.02 (2025-03-12): https://www.eveonline.com/news/view/patch-notes-version-22-02
- CCP official patch notes, "Restructuring Taxes After Relief" (2021-10-19): https://www.eveonline.com/news/view/restructuring-taxes-after-relief
- CCP support article, "Broker Fee and Sales Tax": https://support.eveonline.com/hc/en-us/articles/203218962 (direct fetch 403s outside a browser session; verified via Wayback Machine snapshot dated 2023-12-10: https://web.archive.org/web/20231211134209/https://support.eveonline.com/hc/en-us/articles/203218962)
- ESI live game data: `GET https://esi.evetech.net/latest/universe/types/3446/` (Broker Relations) and `GET https://esi.evetech.net/latest/universe/types/16622/` (Accounting) — description fields are CCP's own current skill text
- ESI OpenAPI spec: `GET https://esi.evetech.net/meta/openapi.json` — used to confirm `/characters/{character_id}/skills/` and `/characters/{character_id}/standings/` request/response schemas and required OAuth scopes
- Fuzzwork SDE type-ID lookup (independent cross-check of type IDs): https://www.fuzzwork.co.uk/api/typeid.php
