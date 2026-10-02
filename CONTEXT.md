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
standings. For a buy recommendation, `B` is the front-of-queue bid; for a sell
recommendation on held stock, `B` is the lot's acquisition price instead —
still a prediction (computed before the sale posts), never to be confused with
realised margin.
_Avoid_: markup, ROI, profit margin (on cost)

**Target net margin**:
The minimum net margin a candidate must clear to be recommended. For a buy
recommendation it's a hard filter at front-of-queue prices, not a lever that
moves them; for a sell recommendation on held stock it's a flag threshold
instead — falling short never excludes the sale, since the capital is already
spent.
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

**Observed capture**:
The fraction of an item's average daily volume actually sold over a period,
measured from realised trades; the calibration target for the capture rate.
_Avoid_: actual capture, realised rate

**Realised margin**:
The net margin actually achieved on a completed round trip, as opposed to the
predicted net margin.
_Avoid_: actual margin, achieved margin

**Expected daily profit**:
`net profit per unit × capture rate × average daily volume` — an item's daily
profit rate, independent of how many units the pilot posts.
_Avoid_: daily yield, potential profit

**Recommendation**:
Either a buy recommendation or a sell recommendation. The two no longer imply
each other: posting a sell order needs held stock a buy order hasn't
necessarily delivered yet.
_Avoid_: signal, call

**Buy recommendation**:
A candidate paired with the front-of-queue buy price, the resulting net
margin, and a unit quantity, subject to the pilot's budget and order limit.
Carries no assumption that a matching sell order follows immediately.
_Avoid_: round trip

**Sell recommendation**:
A type with held-unlisted stock, paired with the front-of-queue sell price and
its full available quantity — never partial, since stock already paid for is
never worth holding back. Bypasses the filter layer and ranking entirely;
flagged, not excluded, when its realised margin falls under target.
_Avoid_: round trip

**Pending**:
A lot or order awaiting an outcome this run cannot act on: an open-buy lot not
yet filled, an open sell order already covering stock (not re-recommended), or
an unknown-outcome lot. Never counted toward funded, unfunded, excluded, or
sell recommendations.
_Avoid_: in-flight, outstanding

**Allocation**:
The selection of which buy recommendations to post and how many units each,
under the pilot's ISK budget, order limit, and buy-order escrow. Sell
recommendations are not allocated — they compete for an order slot, not
budget (see the allocation-of-pre-existing-orders decision).
_Avoid_: portfolio, basket

**Committed capital**:
The ISK a posted buy order ties up: its escrow plus the broker fees of the round
trip. Sales tax is netted from sale proceeds, not committed.
_Avoid_: investment, cost basis

**Days of supply**:
`units ÷ (capture rate × average daily volume)` — the expected number of days an
order size takes to sell; caps the units posted per item.
_Avoid_: turnover, holding time

**Minimum order**:
The smallest committed capital worth posting; a partial fill below it is left
idle rather than created. Default 1M ISK.
_Avoid_: order floor, dust order

### Inventory ledger

**Lot**:
A quantity of one item type acquired via a single buy order at a single
acquisition price, tracked from the moment it starts delivering until every
unit is sold. A buy order fills at one fixed price with no slippage, so
acquisition price only ever varies *across* lots of the same type, never
within one.
_Avoid_: batch, parcel, position

**Ledger**:
The local record of every lot this pilot's own recommendations have bought
and not yet sold — the sell-side source of truth, kept separate from raw ESI
assets, which also hold personal items and stock bought for other characters.
_Avoid_: inventory (broader — covers everything in the hangar, not just
trading stock)

**Held stock**:
A lot's quantity that is both physically delivered and not already reserved
for an open sell order — the only quantity eligible for a new sell-order
recommendation.
_Avoid_: inventory, stock on hand

**Reservation**:
The portion of a lot's quantity claimed by an open sell order; released back
to held stock if that order is cancelled, finalized to sold once it fills.
_Avoid_: hold (ambiguous with allocation)

**Acquisition price**:
The fixed per-unit price a lot's buy order posted at; a lot's basis for
flagging a sale that clears less than the target net margin. Distinct from
committed capital, which also counts escrow and fees.
_Avoid_: cost basis

**Seeded lot**:
A lot entered directly at held-unlisted, via an explicit pilot action rather
than a tracked buy order, for stock that was already in the hangar before the
ledger existed. Carries no acquisition price, so it never triggers the
margin-gate flag.
_Avoid_: imported lot, legacy stock

**Ledger drift**:
A mismatch between the ledger's held-stock quantity for a lot and what ESI's
live assets actually show at the trade station; resolved by clamping the
ledger down to the lower, asset-confirmed figure.
_Avoid_: desync, discrepancy

**Unknown outcome**:
An order that disappeared from the active-orders list with units still
unfilled and no matching cancelled/expired record in order history — ESI
gives no way to tell whether it filled or vanished some other way, so the
ledger surfaces it rather than guessing.
_Avoid_: lost order, orphaned order

### Authentication

**Login**:
The one interactive browser round-trip that establishes the pilot's credentials
at the EVE SSO consent screen. A verb for the pilot's action; the protocol step
it performs is *authorization*.
_Avoid_: sign in, authenticate (as a command name)

**Credentials**:
The pilot's stored OAuth client identity and refresh state, held in
`credentials.json` at mode 600 — the single source of auth (spec §13).
_Avoid_: tokens, auth file

**Client ID**:
The ESI application's public identifier, sent with both the authorization
request and the refresh. Public by design under PKCE; never a secret.
_Avoid_: app key, API key

**Authorization code**:
The one-time code EVE SSO returns to the loopback callback, traded for an
access/refresh token pair. Lives five minutes.
_Avoid_: callback code, auth token

**Access token**:
The short-lived JWT (20 min) minted from a refresh token; its `sub` claim carries
the character id.
_Avoid_: session token

**Refresh token**:
The long-lived token that mints access tokens; it rotates on every exchange, so
the returned one is always persisted.
_Avoid_: long-lived token, offline token
