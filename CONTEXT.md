# EVE Trader

A personal tool for tracking EVE Online market orders and surfacing station-trading opportunities, built against CCP's ESI API.

## Language

**Hub**:
One of the trading locations the tool scans independently — for v1, Jita (station 60003760, Jita IV - Moon 4 - Caldari Navy Assembly Plant, The Forge region) or Rens (station 60004588, Rens VI - Moon 8 - Brutor Tribe Treasury, Heimatar region). Hubs are never combined — an Opportunity, an Item Universe, and a Scan Cycle are each scoped to exactly one Hub.
_Avoid_: Trade hub, station, region (a Hub identifies a specific station, not the wider region ESI's market endpoints report on).

**Margin**:
The net profit a station trade would yield at a Hub for one item: `(best sell order − best buy order) − fees`. Always net of fees — a figure that hasn't had fees subtracted is not yet a Margin. Always a *live estimate* against the current order book — for what a completed trade actually returned, see Realized Margin.
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

**Refresh Cycle**:
One full pass evaluating the character's Orders (firing any due Alerts), fetching new Wallet Transactions, and running a Scan Cycle for every Hub — a superset of Scan Cycle, spanning the scanner, order-tracking, and realized-trade-tracking vocabularies below. Triggered either by a dashboard request or independently on a background schedule; both share the same underlying Alert-throttle state, so one triggering a Refresh Cycle doesn't cause a duplicate Alert the other already fired.
_Avoid_: Tick, poll, sync (this is specifically "evaluate Orders + fetch Wallet Transactions + Scan every Hub", not a generic periodic action).

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

### Realized trade tracking

Distinct from Order tracking above: these terms describe the character's completed buy/sell activity, reconstructed from ESI's wallet-transactions endpoint, not the still-open Orders above or the public order book the scanner reads.

**Wallet Transaction**:
One fill of the character's own buy or sell activity, as reported by ESI's wallet-transactions endpoint: an item, quantity, unit price, Hub, buy-or-sell side, and timestamp. The raw record a Lot and Matched Trade are derived from.
_Avoid_: Transaction (too generic elsewhere in casual use), Fill.

**Lot**:
A fixed-quantity, fixed-price slice of one Wallet Transaction, produced when FIFO matching splits it against a differently-sized counterpart on the other side of the trade — e.g. one 100-unit buy Wallet Transaction sold off across three separate sell Wallet Transactions becomes three buy Lots. The unit FIFO matching actually pairs.
_Avoid_: Batch, chunk.

**Matched Trade**:
One buy Lot paired with an equal-quantity sell Lot of the same item at the same Hub, produced by FIFO cost-basis matching of Wallet Transactions oldest-buy-first. The atomic row of the realized-trade report. Distinct from an Order (still open, unfilled) and an Opportunity (a scanner candidate nobody has acted on yet) — a Matched Trade only exists once both sides have actually happened.
_Avoid_: Trade, round trip.

**Realized Margin**:
A Matched Trade's actual net ISK profit or loss: `(sell Lot's unit price − buy Lot's unit price) × quantity − Fees`, using the same live Fee formula as Margin (ADR-0001/0006) but applied to real historical fill prices instead of the current order book. Distinct from Margin, which is always a forward-looking estimate against the *current* best orders, never a record of what actually happened.
_Avoid_: Margin (reserved for the scanner's live estimate), P&L, profit.

**Realized Markup**:
A Matched Trade's Realized Margin expressed as a fraction of its buy Lot's cost: `Realized Margin / (buy Lot's unit price × quantity)`. The realized-trade-tracking counterpart to Markup, same profit-over-cost framing applied to an actual completed trade instead of a live Opportunity.
_Avoid_: Margin Percentage, Margin %, ROI, return.

**Open Lot**:
A buy Lot (or unsplit buy Wallet Transaction) not yet consumed by any matching sell — stock the character is still effectively holding. Produces no Realized Margin until a future sell matches against it.
_Avoid_: Position, inventory (not otherwise a tracked concept in this app).

**Unmatched Sell**:
A sell Wallet Transaction that FIFO matching cannot pair to any known buy Lot — either because the underlying stock predates the app's tracked history, or the corresponding buy fell outside the window ESI's wallet-transactions endpoint returns. Listed in the realized-trade report on its own, with no cost basis or Realized Margin, rather than silently dropped — its presence signals the report is incomplete for that item.
_Avoid_: Orphaned sell, discarded silently.
