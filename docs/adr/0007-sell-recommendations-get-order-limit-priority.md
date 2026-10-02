# Pre-existing orders reserve headroom first; sell beats buy for what's left

v1's allocation (spec §10) assumed a clean run: the full skill-derived order
limit and the full stated budget were available to allocate. They aren't,
once orders can already be open from a prior run or posted manually. We
extend both resources to account for what's already committed, in two
different ways, because budget and the order limit are consumed by
different things:

- **Order limit** is a flat count (unchanged formula). Every currently-open
  order — buy or sell, this tool's or not — subtracts from it before any of
  this run's new recommendations get a slot.
- **Budget** only shrinks for open *buy* orders: subtract
  `price × volume_remain` (current, not original, escrow — already-paid
  broker fees are sunk) for each one. Open sell orders cost no escrow, so
  they don't touch budget.

The order limit, once reduced, is **shared** between sell recommendations
and new buy recommendations — and sell wins the contention. A sell
recommendation recovers capital that's already spent; a new buy
recommendation deploys capital that hasn't been spent yet. Clearing the sunk
position takes priority over opening a new one. Concretely: reserve a slot
for every sell recommendation first, then run the existing EDP-ranked
allocation (unchanged mechanism) on whatever slots and budget remain for buy
recommendations. If slots run out before every held-stock type gets a sell
recommendation — a real but rare case, since order limits are generous
relative to how many distinct types a pilot typically holds at once — the
excess sell recommendations land in `Pending` (`order-limit-exhausted`)
rather than silently breaking ADR 0005's "always recommend" (which was a
promise that margin never excludes a sale, not a promise that a slot always
exists).

An `unknown-outcome` lot (ADR 0003) is, by definition, absent from
`/orders/` — so it never reserves either resource here. Its fate is
unresolved, not reserved; the run's output should say so, not quietly
assume the escrow or the slot is still held.

The rejected alternative was giving new buy recommendations priority (or
splitting the remainder proportionally) on the theory that EDP-ranked buys
are the tool's actual value proposition. That optimizes for deploying more
capital while sunk capital sits idle waiting for a slot that a lower-value
new buy took first — backwards from what a pilot holding stock would want.
