# Market-history refresh tolerates per-type failures

The Heimatar history refresh fetches one ESI request per type on the current Rens order book (~8,000 types), and a handful of those types permanently 404. An all-or-nothing refresh meant a single 404 discarded the entire window: `market_history` stayed empty, so the v1 volume filter rejected every opportunity. We refresh per type instead — a type whose fetch fails keeps its previous window, the successful types are applied, and only a total failure returns an error. The refresh also waits for a populated order book before its first run, so a first boot does not no-op and then idle until the next daily reset. ([#47](https://github.com/mgoodness/eve-trader/issues/47))

## Considered Options

- **Atomic, all-or-nothing refresh** — rejected: one permanently-failing type wipes the whole cache, which is exactly the production failure this decision fixes.
- **Treat a 404 as "no history" and clear the type** — rejected: it conflates "this type has no market history" with "the fetch failed", so a transient error would destroy good data.
- **Refresh immediately on startup** — rejected: the type set comes from `market_order`, which the order poller fills on its own schedule; refreshing before it exists is a guaranteed no-op.
