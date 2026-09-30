# v1 acceptance snapshot

One frozen live capture of the Heimatar region-orders feed, plus the
jump-distance routes and market history the v1 pipeline needs, committed so
`TestAcceptanceAgainstFrozenSnapshot` (ticket #24) runs offline and
repeatably. The check asserts the functional acceptance criteria in
`docs/spec.md` §14; it makes no claim that the engine's 20% capture rate is
correct.

## Provenance

| Thing | Value |
|---|---|
| Region | Heimatar `10000030` |
| Trade station | Rens VI − Moon 8 − Brutor Tribe Treasury `60004588` |
| Trade system | Rens `30002510` |
| Captured at | 2026-09-30 ~23:28 UTC |
| Compatibility date | `2026-09-30` |
| Source | public ESI (`esi.evetech.net/latest`), **no SSO** |

## What is frozen vs synthesized

**Frozen — captured verbatim from live ESI:**

- `orders-page-01.json.gz` … `orders-page-04.json.gz` — the whole
  region-orders feed (`GET /markets/10000030/orders/?order_type=all`, 71
  live pages, 70,707 orders, 10,341 distinct types), compacted to the
  fields the engine reads and split into four transport pages so each file
  stays under the repo's large-file hook. The test's fake ESI server serves
  them back page-by-page with `X-Pages: 4`, exercising the real multi-page
  fetch/decode path. This is the one input the acceptance check is *about*.
- `routes.json` — `GET /route/{system}/30002510/` for the 35 systems that
  appear in a numeric-range buy order at an NPC station. The engine's jump
  distance is `len(route) − 1`.
- `history.json.gz` — `GET /markets/10000030/history/?type_id=N` for the
  478 types that survive the book-only filter stage at the frozen
  parameters. Each type keeps its trailing 30 daily records (the window the
  history-dependent filters read), so the committed file is the filter's
  input, not ESI's full 425-day response.

**Frozen by the spec, served as fixtures (no live token):**

- The pilot's skills and standings (spec §4): character `932683762`,
  Trade 4 / Broker Relations 4 / Accounting 3, zero standings → broker
  **1.8%**, sales tax **5.025%**, order limit **21**. The fake server mints a
  fake SSO token and serves these skills, exactly as the other CLI tests do.
  No `credentials.json` or live token is required.

**Not synthesized:** nothing in the market data. The check exercises the
real feed, real routes, and real history from the capture above.

**Deliberately not claimed:** the flat 20% capture rate (spec §9, §14).
`Meta.Params.CaptureRate` echoes the engine's assumption; the check only
verifies the structural acceptance criteria.

## Observed acceptance run

At the pilot's default budget (150,000,000 ISK): 3,957 two-sided types →
**1 funded** (committing 149,999,822 ISK), 164 unfunded, 3,792 excluded,
**2 of 21 order slots** used. Because the budget — not the slots — binds
that run, the check additionally runs the same snapshot at a
slot-binding budget (10,000,000,000 ISK), which funds 10 candidates across
20 slots, so the order-limit criterion is exercised rather than vacuous.

## Refreshing

The snapshot is intentionally frozen; the file hashes below are what the
check was validated against. To recapture (public ESI only):

1. fetch every page of
   `GET /markets/10000030/orders/?order_type=all&page=N` (read `X-Pages`
   from page 1), compact to
   `{order_id,type_id,location_id,system_id,volume_remain,min_volume,price,is_buy_order,range}`,
   concatenate the pages into one array, split it into four JSON arrays,
   gzip each → `orders-page-01..04.json.gz`;
2. fetch `GET /route/{system}/30002510/` for each system in a numeric-range
   NPC-station buy order → `routes.json`;
3. run the book-only filter stage over the refreshed feed at the frozen
   parameters (δ 100, target margin 10%, gross ceiling 80%, thin book 2
   orders within 5%) and fetch `GET /markets/10000030/history/?type_id=N`
   for each survivor, keeping the trailing 30 records → `history.json.gz`.

Re-capturing changes the frozen hash and may change the observed counts; the
check's assertions are invariants and should still hold.

## Integrity

```
sha256  orders-page-01.json.gz  4c4cdd7d3567f65b0f14d7f3166df8e687e57ece8cc0b2442b41d4e17583b11e
sha256  orders-page-02.json.gz  1213877f0e2fdc87bec9e0b557a60e8f4fa65d80e5bbc0a85b2e50f328e93858
sha256  orders-page-03.json.gz  c7e4b63a31b26e5ae6827218a6abb64d38059f82a001f24c77e6ebe92b38fb02
sha256  orders-page-04.json.gz  32232a5582cd21bc91c279a731d8bb99b86520466e71a71b40fc3a583610f926
sha256  history.json.gz         a3a8f99584929dca211559ee394499fac48e46708728d64edb750abe458040b7
sha256  routes.json             0ced2126f1f6c736d750e984ff607d4821a26189284a964e750ae11d8a991664
```
