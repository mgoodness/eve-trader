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

**Portfolio**:
The view that tracks the trading character's own market activity and profit/loss, kept beside the Opportunity list.
_Avoid_: wallet, account

**Position**:
The quantity of one item held at one location, together with its average cost. The unit of the Portfolio's rows.
_Avoid_: holding, lot, stock

**Disposition**:
How a position leaves: a sale (market) or a transfer (off-market).
_Avoid_: exit, closure

**Transfer**:
Goods that leave the character without a market sale — detected from item-exchange contracts, or recorded manually when ESI cannot see them.
_Avoid_: sale, giveaway, handoff

**Average cost**:
The weighted-average cost of the units held in a position, per (item, location), including buy-side fees.
_Avoid_: FIFO cost, cost basis

**Realized P/L**:
Profit or loss on units that have left a position, net of broker fees and sales tax.
_Avoid_: profit, gains

**Unrealized P/L**:
Profit or loss on units still held or listed, valued at the Rens best sell net of estimated sell fees.
_Avoid_: paper profit, open P/L

**Unattributed fees**:
The gap between the journal's actual broker fees and sales tax and the per-item estimated fees; mostly re-lists, which ESI does not link to an order.
_Avoid_: unallocated costs, missing fees

**Sunk fees**:
Broker fees paid on orders that yielded no position (cancelled or zero-fill), and the unfilled remainder of partially filled orders.
_Avoid_: dead costs, write-offs

**Re-list**:
Changing a resting order's price, either by an in-place modify or by cancelling and recreating it.
_Avoid_: bump, update, amend

**In-place modify**:
A re-list that keeps the order's `order_id` and moves its `issued` time; distinct from a cancel-and-recreate, which makes a new order.
_Avoid_: edit, price change

**Resting order**:
One of the character's own open market orders, as opposed to the public Rens book.
_Avoid_: live order, listing

**Relist gain**:
The net increase from raising a resting sell order to the current best sell, after the re-list fee and extra sales tax. Computed only when positive.
_Avoid_: relist profit, bump gain

**Break-even price**:
The list price at which a position's net proceeds cover its average cost and allocated estimated fees — zero profit. Shown as a low/high range: the low uses only confidently allocated fees, the headline high also shares the unattributed-fee bucket.
_Avoid_: floor price, minimum price

**Target price**:
The break-even price plus the target net margin. Shown over the same low/high range as the break-even price.
_Avoid_: ask price, goal price

**Target net margin**:
The view-level, URL-carried net margin a Target price must earn, defaulting to 0% net so Target equals Break-even.
_Avoid_: desired margin, target profit

**Market-implied net margin**:
The net margin a position would actually realize at the current Rens best sell.
_Avoid_: current margin, market margin

**Ledger**:
The append-only local store of the character's raw ESI wallet, order, and contract records, from which positions and P/L are derived.
_Avoid_: history, database, cache

**Manual transfer**:
A user-entered transfer record for goods ESI cannot see leave the character (in-game direct trades).
_Avoid_: manual entry, adjustment

**Broker-fee rate (R_b)**:
The fraction of an order's value charged as a broker fee, derived from the Broker Relations skill (and, in the standings fast-follow, faction and corporation standings).
_Avoid_: broker tax, commission

**Standing**:
The character's unmodified NPC faction or corporation reputation, used by the standings fast-follow to reduce the broker-fee rate.
_Avoid_: reputation, faction standing
