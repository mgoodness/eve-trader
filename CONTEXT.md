# EVE Trader

A personal tool for tracking EVE Online market orders and surfacing station-trading opportunities, built against CCP's ESI API.

## Language

**Hub**:
One of the trading locations the tool scans independently — for v1, Jita (station 60003760, Jita IV - Moon 4 - Caldari Navy Assembly Plant, The Forge region) or Rens (station 60004588, Rens VI - Moon 8 - Brutor Tribe Treasury, Heimatar region). Hubs are never combined — an Opportunity, an Item Universe, and a Scan Cycle are each scoped to exactly one Hub.
_Avoid_: Trade hub, station, region (a Hub identifies a specific station, not the wider region ESI's market endpoints report on).

**Margin**:
The net profit a station trade would yield at a Hub for one item: `(best sell order − best buy order) − fees`. Always net of fees — a figure that hasn't had fees subtracted is not yet a Margin.
_Avoid_: Spread (the pre-fee sell-minus-buy figure, a distinct intermediate value), profit.

**Fees**:
The broker fee (paid twice per round-trip station trade — once placing the buy order, once placing the sell order) and sales tax (paid once, on the sale) that reduce a trade's proceeds. Both are specific to the character's own skills and standings, not a fixed game-wide rate.
_Avoid_: Tax (ambiguous between broker fee and sales tax — name the specific one).

**Opportunity**:
One ranked entry in the scanner's output: a single item at a single Hub, together with its Margin and other ranking fields, having passed the configured minimum-volume and minimum-margin filters. The same item can produce a separate Opportunity at each Hub independently.
_Avoid_: Deal, listing, result.

**Item Universe**:
The bounded set of items the scanner evaluates at a given Hub. Distinct from an Opportunity: every item in the Item Universe gets evaluated, but only the ones that pass the filters become Opportunities.
_Avoid_: Item list, watchlist.

**Volume Window**:
The trailing period of daily trade volume the scanner averages to judge an item's liquidity at a Hub.
_Avoid_: Volume period, lookback.

**Scan Cycle**:
One full recomputation of a Hub's ranked Opportunity list.
_Avoid_: Refresh, poll, tick.
