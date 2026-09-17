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
The refresh of the rolling 14-day Heimatar volume window for every type on the order book.
_Avoid_: order poll, sweep, backfill

**Expected daily profit (ISK/day)**:
Profit per unit multiplied by average daily volume; the primary rank of the Opportunity list.
_Avoid_: EDP, profit
