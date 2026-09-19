# eve-trader

A single-user tool that finds station-trading opportunities at Rens in EVE Online's Heimatar region.

## Language

**Opportunity**:
An item that has both a best buy order and a best sell order at Rens, clears the always-on realism filters, and clears the active user filters.
_Avoid_: trade, deal, flip

**User filter**:
One of the four trader-adjustable bounds applied after the realism filters: minimum daily volume, minimum and maximum gross margin %, and maximum sell price. Bounds are inclusive and a blank control means no bound.
_Avoid_: filters, criteria, v1 thresholds

**Rens**:
The NPC station in the Heimatar region where every trade in scope takes place.
_Avoid_: station, market

**Heimatar**:
The region whose market history supplies the volume figures; volume is region-wide, not station-specific.
_Avoid_: region, market

**Order poll**:
The refresh of the current Rens order book — what is on the market right now.
_Avoid_: history refresh, sweep, snapshot

**History refresh**:
The refresh of the rolling 30-day Heimatar volume window for every type on the order book.
_Avoid_: order poll, sweep, backfill

**Expected daily profit (ISK/day)**:
Profit per unit net of broker fee and sales tax, multiplied by average daily volume and by the capture rate; the primary rank of the Opportunity list.
_Avoid_: EDP, profit

**Net margin**:
Profit per unit after broker fee and sales tax, as a percentage of sell price; the margin figure the table displays.
_Avoid_: margin, profit margin

**Capture rate**:
The fixed ~20% share of an item's average daily volume a single trader is assumed to capture when computing Expected daily profit. A deliberate v1.1 assumption, not a user control.
_Avoid_: volume share, fill rate

**Realism filter**:
An always-on, exclude-only check applied before ranking: an item is dropped from the list unless its history and order book are trustworthy. Not user-adjustable and has no reveal toggle.
_Avoid_: auto filter, hidden filter

**Trade-day**:
A day in the retained market-history window on which at least one order was placed (order count > 0). The unit of the thin-history and manipulated-history checks.
_Avoid_: trading day, active day

**Thin history**:
An item with fewer than 7 trade-days over the retained window; hidden automatically by a realism filter.
_Avoid_: low volume, dead market

**Manipulated history**:
An item with fewer than 14 trade-days and a high/low price swing greater than 20×; hidden automatically by a realism filter.
_Avoid_: volatile history, price spike

**Thin book / single-order spread**:
A spread whose buy or sell side rests on one or fewer orders within 5% of that side's best price; hidden automatically because it will not survive the first fill.
_Avoid_: shallow book, illiquid spread

**Incomplete history**:
A retained window that lacks the price fields (average/highest/lowest), typically because the type has not been re-fetched since the migration that added them. Hidden automatically until complete.
_Avoid_: stale history, missing history
