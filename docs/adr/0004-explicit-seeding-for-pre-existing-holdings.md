# Ledger bootstrap is explicit and leaves acquisition price unknown

A pilot adopting the ledger (ADR 0003) already has stock sitting in the Rens
hangar that eve-trader never bought — confirmed live on the real character
before this ADR was even written. Left alone, the ledger's own rule ("only
what this tool transacted is trading stock") makes that stock permanently
invisible: never recommended for sale, forever.

We resolve this with a **seeded lot** — a lot entered directly at
`held-unlisted` (skipping `open-buy`, since there is no buy order to track)
via `source: seeded` instead of a `source_order_id`, and with
`acquisition_price: null`. Seeding only ever happens through an **explicit,
pilot-driven action** that lists untracked `Hangar` stock and asks the pilot
to confirm each item is actually trading stock — never automatically, and
never as a side effect of an ordinary run.

The obvious alternative — auto-absorb anything sitting in the `Hangar` that
the ledger doesn't already know about — is rejected on purpose. It would
quietly undo the entire reason the ledger exists instead of trusting raw ESI
assets: a character's hangar holds personal items and stock bought for other
characters alongside trading stock (live-confirmed: only 4 of 40 asset rows
at Rens were actually `Hangar` stock, and nothing in that signal distinguishes
trading stock from the rest). Only the pilot knows which is which.

A seeded lot's `acquisition_price` stays `null` rather than being
reconstructed from wallet-transaction history or asked for manually. A
lookback hits the same fee/transaction-attribution ambiguity the
wallet-transactions research already found (broker fee can't be reliably
tied back to one transaction); manual recall is unreliable friction for what
is a one-time import path. The margin-gate/cost-basis decision's flag simply
never fires for a lot with no known acquisition price — consistent with this
ledger's existing "surface unknown rather than guess" rule for fill
detection (ADR 0003).
