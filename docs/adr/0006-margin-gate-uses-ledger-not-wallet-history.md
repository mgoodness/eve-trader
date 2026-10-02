# The margin-gate flag uses the ledger and current skill rates, not wallet history

A sell recommendation on held stock is always posted (ADR 0005), but flagged
when its margin falls short of target. Computing that margin needs a cost
figure to stand in for `B` in the existing net-margin formula (`CONTEXT.md`) —
and it turns out the ledger already has exactly that, with no ESI wallet call
needed: a lot's `acquisition_price` is the price *our own* buy order posted
at, and a buy order fills at that price with no slippage (ADR 0003). Broker
fee and sales tax are computed analytically from the pilot's *current*
skills and standings, exactly as the existing buy-side filter already does
(spec §8) — never read from a historical wallet-journal entry.

This is worth recording because the charting session assumed the opposite:
the wallet-transactions research ticket exists specifically because we
expected to need `/wallet/transactions/` and `/wallet/journal/` for this.
That research is not wasted — a lot's acquisition price could in principle
drift from reality (a manual trade, a price correction) and wallet data
would be the way to catch that — but it answers a different question
(ex-post validation: did the predicted margin match what actually happened),
which is out of scope for this map and deferred to the v2 tracker named in
spec §15. The research's finding that **broker fee can't be reliably tied
back to a specific transaction** (`esi/esi-issues#82`) is also the reason a
historical approach would have been materially harder than the analytical
one, not just unnecessary.

A multi-lot sell recommendation's margin is a quantity-weighted average of
its lots' individual acquisition prices — matching "one recommendation, one
sell order" (ADR 0005) rather than fragmenting one type into several rows.
If any of its lots are seeded (ADR 0004, no acquisition price on file), the
weighted average is computed over only the priced quantity, with the
unpriced quantity surfaced explicitly (e.g. "margin computed on 50/150
units") rather than silently dropped or guessed at.

The resulting figure is still a **net margin** — the same term and formula
`CONTEXT.md` already defines, just with a lot's acquisition price standing in
for a front-of-queue bid — not a new "realised margin." That term is already
claimed for the true, post-completion figure the v2 validation hook computes
from actual wallet data; reusing it here for a pre-sale prediction would
collide with that meaning.
