# Buy and sell recommendations are distinct types, not one paired shape

v1's `Recommendation` paired a buy price and a sell price as one round trip,
implicitly assuming both sides are postable in the same run. They aren't: a
sell order needs stock a buy order hasn't necessarily delivered yet (the
whole reason this map exists). We split `Recommendation` into two
independent types, `BuyRecommendation` and `SellRecommendation`, rather than
keeping one shape with optional buy/sell legs.

The split goes deeper than the schema. The two sides no longer share a
pipeline:

- **Buy-side** is v1 unchanged, understood as one leg rather than half a
  round trip: filter layer (spec §7) → pricing rule → EDP ranking → budget/
  order-limit allocation → funded/unfunded/excluded.
- **Sell-side** bypasses the filter layer and ranking entirely. It always
  recommends the *full* held-unlisted quantity for every type that has any
  (never partial — stock already paid for is never worth holding back), at
  the current front-of-queue price, flagged rather than excluded when
  realised margin falls under target. There is nothing to rank by expected
  daily profit and nothing to ration against a budget, because selling
  commits no new capital.

A type_id can independently appear in the buy list, the sell list, both, or
neither in the same run — buying more of something you're also currently
selling is a normal, simultaneous state, not a conflict a shared shape would
need to reconcile.

A third bucket, `Pending`, covers what's in flight either way and isn't
actionable this run: an open-buy lot not yet filled, an open sell order
already covering stock (not re-recommended), or a lot whose fill outcome ESI
can't resolve (ADR 0003's "unknown outcome"). One bucket with a `reason`
field, mirroring the existing `excluded` bucket's count-by-default/detail-
under-`--explain` shape, rather than three separate lists.

The rejected alternative was one `Recommendation` shape carrying optional
buy and sell legs. It would have forced the sell side's unconditional,
unranked, unbudgeted behaviour to coexist with the buy side's filtered,
ranked, budget-rationed behaviour inside the same type — hiding a real
difference in how the two sides work behind a shape that implies they're
symmetric.
