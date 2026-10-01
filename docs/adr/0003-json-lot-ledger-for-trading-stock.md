# A JSON lot-ledger tracks trading stock; ESI assets are never trusted directly

`eve-trader` gains a local, persistent trading-stock ledger — the first user
state the project has ever kept (spec §13 previously said "no user state").
It stores one **lot** per buy order that has delivered or is delivering stock
— `{lot_id, type_id, source_order_id, quantity_total, quantity_available,
acquisition_price, acquired_at, status}` — rather than one row per type_id,
because a buy order fills at one fixed price with no slippage, so acquisition
price only ever varies *across* lots of the same type, never within one. A
sell order **reserves** quantity from the oldest available lot(s) (FIFO);
the reservation releases back to `quantity_available` if the sell order is
cancelled, and finalizes to `sold` once it fills. Sold lots are kept, not
deleted — the realised-fill history a later cost-basis/margin-gate decision
and the v1 spec's §15 validation hook will both want.

Storage is a flat JSON file (`~/.local/state/eve-trader/ledger.json`,
atomic-write via temp-file-then-rename, matching `credentials.json`'s
pattern), not embedded SQLite. The scale forced this: the skill-derived order
limit caps a character around 21–30 active orders total, so the ledger never
holds more than a few dozen rows. A real database buys transactional
multi-row updates and indexed queries neither of which this scale needs, at
the cost of a new dependency; the lot schema carries over unchanged if a
future effort (v2 relist/alerts) ever needs to migrate to one.

The ledger is reconciled once per run, at its start: poll `/characters/{id}/orders/`
and diff against each lot's last-known `volume_remain`, then poll
`/characters/{id}/assets/` filtered to the Rens station's `Hangar` flag as a
ceiling check. ESI assets are **never** the sell-side source of truth — a
character's hangar also holds fitted-ship clutter and stock bought for other
characters (live-confirmed: only 4 of 40 asset rows at the trade station were
actually `Hangar` stock) — only the ledger's own bookkeeping says what this
tool bought and hasn't sold. If assets show less than the ledger's
held-unlisted quantity, the ledger's quantity is clamped down to match, with
a visible warning; assets showing more than the ledger expects is untracked
clutter and is never pulled in.

Fill detection is an inference, not an ESI guarantee, because
`/orders/history/`'s `state` enum has no `filled` value — CCP's own tracker
(`esi/esi-issues#612`) confirms the gap is scope, not a documentation
oversight. The algorithm: a `volume_remain` decrease on a still-listed order
is an authoritative partial fill; when an order disappears, check
`/orders/history/` for it — `cancelled`/`expired` there is authoritative (the
last-seen `volume_remain` never arrives); absent from history with
last-seen `volume_remain: 0` is a confirmed full fill; absent from history
with `volume_remain > 0` is an **unknown outcome** the ledger must surface
rather than guess at, since no primary source resolves that case either way
(`docs/research/esi-assets-and-orders.md` §2.3.4).

The rejected alternative was trusting `/orders/history/`'s absence as "must
have filled," which several community reports (quoted in the research note)
show failing silently for orders repriced enough times to outlive its 90-day
window, or for orders that simply vanish with no recorded reason.
