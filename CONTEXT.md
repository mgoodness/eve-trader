# eve-trader

A single-user tool that finds station-trading opportunities at Rens in EVE Online's Heimatar region.

## Language

**Opportunity**:
An item that has both a best buy order and a best sell order at Rens and whose margin and volume clear the v1 thresholds.
_Avoid_: trade, deal, flip

**v1 thresholds**:
The fixed filters an Opportunity must clear: 5% minimum margin and 10 units/day minimum average volume.
_Avoid_: filters, criteria

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
