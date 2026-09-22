# Heimatar region as the pricing market

Status: accepted

v1 scoped every trade to Rens station and explicitly excluded regional
arbitrage (docs/spec/v1.md §2), because station trading was the intended
activity. We now rank on the **whole Heimatar region** instead: the
Opportunity list's best buy is the highest buy order anywhere in the
region, and its best sell the lowest sell order anywhere, with margin and
ISK/day computed from that pair. The order poll stores the entire region
book rather than filtering to Rens, and `market_order` gains a
`location_id` so consumers that must stay Rens-anchored can still filter.

## Considered options

- **Per-station markets, best station wins** (buy and sell at one station,
  chosen across the region). Rejected: the goal was the region's price
  levels, not a menu of venues.
- **Region as reference only** (keep Rens prices in the ranking, show
  region figures as extra columns). Rejected: the list should *be* the
  region view, not annotate a Rens view.
- **Chosen: region as the priced market.** The highest-region buy and
  lowest-region sell are the row's prices and drive the rank.

## Consequences

- **The list is decoupled from the execution venue.** The character's
  orders are still placed and filled at Rens, so the Opportunity list can
  surface items with no Rens market at all. This is deliberate: the list is
  a region-wide pricing signal, not a claim that every row is fillable at
  Rens.
- **The margin is an upper bound, not a realized station-trading spread.**
  Region sell minus region buy assumes fills at the region extrema at Rens,
  which Rens depth may not support. Fees themselves remain Rens-only,
  because orders are placed there.
- **Hauling is not modeled.** Cross-station price gaps are not netted
  against cargo cost or time; the two stations of the extrema are not even
  surfaced. A region row can therefore describe a spread no Rens trader can
  capture.
- **The realism filters key off the region book.** The single-order-spread
  check counts region orders near the region bests, so it no longer
  measures Rens's own depth.
- **The Portfolio stays Rens-anchored.** `ledger` filters
  `market_order.location_id = 60004588` for unrealized P/L and re-list gain,
  so positions are never marked to a price at a station they do not sit at.
- [ADR-0004](./0004-standings-in-broker-fee-rate.md)'s single-station owner
  assumption still holds: fees are computed against Rens's owner, since
  that is where every order rests.
