# EVE Online sales-tax base rate & Accounting formula

**Question:** What is the current base sales-tax rate in EVE Online, and the
correct `R_t` formula as a function of the Accounting skill?

**Answer (one line):** Base sales tax is **7.5%**, reduced 11% per Accounting
level → **3.375% at level V**. eve-trader's hardcoded formula is **correct and
current**; EVE University's "8% base / 3.6% at V" is stale.

Investigated 2026-09-18. Live server (Tranquility) values.

---

## 1. Both facts, from one authoritative CCP source (ESI)

CCP's official API returns the in-game description of the **Accounting** skill
(typeID 16622). ESI (`esi.evetech.net`) is a first-party CCP service serving live
Tranquility data.

> `GET https://esi.evetech.net/latest/universe/types/16622/?datasource=tranquility&language=en`
> `"description": "... Each level of skill reduces sales tax by 11%. Sales tax starts at 7.5%."`

- Fetched 2026-09-18; response `last-modified: Thu, 17 Sep 2026`.
- This single primary source confirms **both** the 7.5% base **and** the
  11%-per-level Accounting reduction, currently in effect.

## 2. Base rate history — primary CCP patch notes

The 7.5% base was set on **2025-03-12** (patch notes version 22.02).

Official CCP patch notes page
<https://www.eveonline.com/news/view/patch-notes-version-22-02> — under the
"Science & Industry" section (embedded Contentful/JSON in the page):

> "Sales Tax has been increased from 4% to 7.5%."

(Corroborated by the JP-translation forum thread
<https://forums.eveonline.com/t/479540>, which links to the same official
patch-notes URL for the 2025-03-12.1 release.)

Prior values, per EVE University's history log (each line cites a CCP patch note;
secondary aggregation, primary-sourced):
<https://wiki.eveuniversity.org/Trading> →
- v22.02 (2025-03-12): 4% → **7.5%** (current)
- v22.01 (2024-07-25): 8% → 4.5%
- v19.09 (2021-10-19): max sales tax 2.5% → 8%
- v19.05 (2021-07-21, "Grand Heist"): temporary halving 5% → 2.5%
- Aug 2019: max sales tax 2% → 5%; **Accounting reduction raised 10% → 11% per level**

So the base has moved 2% → 5% → (temp 2.5%) → 8% → 4.5% → **7.5%**. The 7.5%
value is what is live now, confirmed directly against ESI (§1) and CCP patch
notes (this section).

## 3. Per-level Accounting reduction (11%)

Confirmed primary via ESI skill description (§1: "reduces sales tax by 11%" per
level). Historically set by the **Aug 2019** patch (10% → 11% per level), per the
EVE University history log at <https://wiki.eveuniversity.org/Trading>:

> "Accounting – Increase in reduction of sales tax from 10% per level to 11% per level"

## 4. The formula to use

```
R_t = 0.075 × (1 − 0.11 × AccountingLevel)
```

| Accounting | R_t     |
|-----------:|---------|
| 0          | 7.500%  |
| 1          | 6.675%  |
| 2          | 5.850%  |
| 3          | 5.025%  |
| 4          | 4.200%  |
| 5          | **3.375%** |

At level V: `0.075 × (1 − 0.55) = 0.075 × 0.45 = 0.03375` = **3.375%**.

## 5. Verdict vs. eve-trader

`ranking/ranking.go` → `SalesTaxRate`:

```go
return 0.075 * (1 - 0.11*float64(accountingLevel))
```

**Correct and current.** No change needed. The 7.5% base matches the live game
(§1, §2) and the 11%-per-level reduction matches CCP's own skill description.

## 6. Resolving the reported conflict

- eveprofits.com / eve-trader (7.5% base, 3.375% @V) — **matches current game**.
- EVE University's *Accounting skill blurb* ("reduces … from 8% to 3.6% at level
  5") — **stale**: 3.6% = 8% × 0.45, using the pre-2025-03-12 (v19.09–v22.01)
  8% base. Note EVE Uni's own *history log* on the same page already records the
  2025-03-12 change to 7.5%, so the wiki is internally inconsistent — its history
  section is up to date, its skill blurb is not.

## Confidence

- **Primary/confirmed:** 7.5% base and 11%/level — CCP ESI live data (§1) plus
  CCP official patch notes for the 7.5% change (§2).
- **Secondary (primary-cited):** the full historical timeline and the Aug-2019
  origin of the 11% figure — via EVE University's patch-note log, which cites CCP
  notes but was not each individually re-fetched from CCP here.
