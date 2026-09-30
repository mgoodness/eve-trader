# eve-trader

A station-trading recommendation engine for EVE Online: given live market data
and the pilot's character skills and standings, it recommends what to post at the
front of the book while still clearing a target net margin.

## Language

### Market mechanics

**Trade station**:
The single station where the pilot buys and sells; for v1, Rens VI − Moon 8 −
Brutor Tribe Treasury (`60004588`) in the Heimatar region (`10000030`).
_Avoid_: hub (ambiguous), market (see below)

**Region market**:
The set of all orders in a region, visible region-wide. It is a *data scope*,
not a venue; goods do not move to it.
_Avoid_: market (unqualified)

**Order**:
A market order — a buy order or a sell order. Never a sequence or a queue
position; say *rank* or *sort* for those.
_Avoid_: using "order" for sequence, priority, or line position

**Buy order**:
An order asking for an item to be provided within the range set on the order, in
exchange for ISK. It reserves 100% escrow on creation.
_Avoid_: bid (reserve for the best buy price), WTB

**Sell order**:
An order offering an item for a fixed price at the station where the order was
created. It has no range.
_Avoid_: ask (reserve for the best sell price), WTS

**Range**:
A buy order's setting for how far from its own station a seller may be to fill
it: station, N jumps, or region. A sell order has no range.
_Avoid_: reach, distance

**Jump distance**:
The number of gate jumps along the shortest route between two solar systems; a
buy order with a numeric range covers the trade station when its jump distance
is at most that range.
_Avoid_: route length, distance

**Effective buy book**:
Every buy order whose range covers the trade station, wherever it sits in the
region. These are the competing bids, and also the prices at which held stock
can be sold immediately without hauling.
_Avoid_: buy orders (unqualified)

**Effective sell book**:
The sell orders located at the trade station. Sellers at other stations are not
competitors for the trade station's buyers.
_Avoid_: sell orders (unqualified)

**Best bid**:
The highest price in the effective buy book.
_Avoid_: top buy, highest buy

**Best ask**:
The lowest price in the effective sell book.
_Avoid_: top sell, lowest sell

**Spread**:
`best ask − best bid`, in ISK per unit.

### Costs and limits

**Broker fee**:
A charge on creating (and on modifying) any buy or sell order, as a percentage
of order value; on an NPC station it falls with Broker Relations and standings,
floor 1%, minimum 100 ISK.
_Avoid_: commission, listing fee

**Sales tax**:
A charge on the seller when an item sells, `7.5% × (1 − 0.11 × Accounting)`,
independent of station.
_Avoid_: transaction fee, market tax

**Relist fee**:
The broker fee charged again when an existing order's price is modified,
discounted by Advanced Broker Relations.
_Avoid_: edit fee, update fee

**Escrow**:
ISK reserved against a buy order at creation, equal to the full order value.
_Avoid_: collateral, deposit

**Order limit**:
The maximum number of simultaneous active orders:
`5 + 4×Trade + 8×Retail + 16×Wholesale + 32×Tycoon`.
_Avoid_: order slots, market slots

**Standings**:
The pilot's unmodified faction and corporation reputation with the station
owner, which reduce the broker fee (0.03% and 0.02% per point respectively).
_Avoid_: reputation, social standing

### Strategy

**Net margin**:
Profit per unit as a fraction of sell price:
`(S − B − broker·B − broker·S − tax·S) / S`, fees taken at the pilot's skills and
standings.
_Avoid_: markup, ROI, profit margin (on cost)

**Target net margin**:
The minimum net margin a candidate must clear to be recommended. It acts as a
filter at front-of-queue prices, not as a lever that moves them.
_Avoid_: required return, threshold profit

**Aggression tick (δ)**:
The ISK step by which a recommendation beats the book: recommend
`best bid + δ` and `best ask − δ`. Configurable; default 100 ISK.
_Avoid_: undercut, overbid, 0.01 tick

**Front-of-queue price**:
The pair `(best bid + δ, best ask − δ)` — the most aggressive prices that still
hold time/price priority.
_Avoid_: best price, competitive price

**Candidate universe**:
The types that have an active order in the region, from which candidates are
drawn.
_Avoid_: item list, type list

**Candidate**:
An item that has passed the filter layer and so is eligible for a
recommendation.
_Avoid_: opportunity, pick

**Capture rate**:
The fraction of an item's average daily traded volume the engine assumes the
pilot can win, used to turn per-unit profit into expected daily profit. Flat in
v1; a fill-probability model is v2.
_Avoid_: fill rate, fill probability

**Expected daily profit**:
`net profit per unit × capture rate × average daily volume` — an item's daily
profit rate, independent of how many units the pilot posts.
_Avoid_: daily yield, potential profit

**Recommendation**:
A candidate paired with the front-of-queue prices, the resulting net margin, and
a unit quantity, subject to the pilot's budget and order limit.
_Avoid_: signal, call

**Allocation**:
The selection of which recommendations to post and how many units each, under
the pilot's ISK budget, order limit, and buy-order escrow.
_Avoid_: portfolio, basket

**Committed capital**:
The ISK a posted buy order ties up: its escrow plus the broker fees of the round
trip. Sales tax is netted from sale proceeds, not committed.
_Avoid_: investment, cost basis

**Days of supply**:
`units ÷ (capture rate × average daily volume)` — the expected number of days an
order size takes to sell; caps the units posted per item.
_Avoid_: turnover, holding time
