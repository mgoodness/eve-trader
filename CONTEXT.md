# EVE Trader

A personal tool for tracking EVE Online market orders and surfacing station-trading opportunities, built against CCP's ESI API.

## Language

**Hub**:
One of the trading locations the tool scans independently — for v1, Jita (station 60003760, Jita IV - Moon 4 - Caldari Navy Assembly Plant, The Forge region) or Rens (station 60004588, Rens VI - Moon 8 - Brutor Tribe Treasury, Heimatar region). Hubs are never combined — an Opportunity, an Item Universe, and a Scan Cycle are each scoped to exactly one Hub.
_Avoid_: Trade hub, station, region (a Hub identifies a specific station, not the wider region ESI's market endpoints report on).

**Margin**:
The net profit a station trade would yield at a Hub for one item: `(best sell order − best buy order) − fees`. Always net of fees — a figure that hasn't had fees subtracted is not yet a Margin.
_Avoid_: Spread (the pre-fee sell-minus-buy figure, a distinct intermediate value), profit.

**Markup**:
An Opportunity's Margin expressed as a fraction of its best buy order price: `Margin / best buy order price`. A return-on-capital view, distinct from the absolute-ISK Margin — the two are filtered independently, and an Opportunity must clear both configured floors (where each is non-zero) to be kept.
_Avoid_: Margin Percentage, Margin %, ROI, return (Markup is the standard term for profit-over-cost, keeping it distinct from Margin's profit-over-revenue framing).

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

### Order tracking

Distinct from the scanner vocabulary above: these terms come from the character's own placed orders (ESI's character-orders endpoint), not the public market-order-book the scanner reads.

**Order**:
One of the character's own live buy or sell orders at a Hub. Not an Opportunity — an Opportunity is a scanner-derived candidate nobody has acted on yet; an Order is something the character actually placed.
_Avoid_: Listing, position.

**Alert**:
A per-Order notification — one of Undercut, Price-Moved, or Expiring — delivered both in-app and via Discord webhook. Fires once on new detection, then suppresses repeats for that Order and alert type for 4 hours or until the condition resolves, whichever comes first.
_Avoid_: Notification (too generic — always name the specific Undercut/Price-Moved/Expiring type once one applies).

**Undercut**:
The Alert condition where a competing order now beats the character's Order by any amount, down to the 0.01 ISK minimum increment, at the same Hub and item.

**Price-Moved**:
The Alert condition where the prevailing market price (the best order on the character's side) has drifted more than 5% from the price the character's Order was originally placed at. Distinct from Undercut: Price-Moved can fire even when the Order hasn't technically been beaten yet.

**Expiring**:
The Alert condition where the character's Order is within 24 hours of its expiration.
