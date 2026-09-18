# prototype-filter-ui (THROWAWAY)

Prototype for [Prototype: filter-form UI + excluded-item presentation](https://github.com/mgoodness/eve-trader/issues/62).

**Question:** What should the v1.1 filter form look like, how should excluded items read to the user, and how does filter state coexist with the sortable column headers?

**Plan:** three structurally different variants of the same page, switchable via `?variant=` on one route.

| Variant | Name | Shape |
|---|---|---|
| A | Toolbar strip | one horizontal filter bar above a full-width table; summary inline |
| B | Sidebar panel | vertical filter panel left, always-on realism card, table right |
| C | Chips + disclosure band | active-filter chips, expandable "why hidden" band, form below |

## Run

```sh
go run ./cmd/prototype-filter-ui
# open http://localhost:8090/?variant=A
```

Flip variants with the floating bar at the bottom (or the ← / → arrow keys).

## What's real

Filtering and sorting are server-side over fake data, so the stateless query
contract from issue #60 is exercised end-to-end:

- **absent param → default** (`minvol=20`, `minmargin=7`, `maxmargin=60`)
- **present-but-empty → no bound** (`...&minvol=`)
- **sort + filters compose in one query string** — the filter form carries a
  hidden `sort`; every column header `hx-include`s `#filterform`. Click a
  header with filters set and watch both survive, and the URL (push-state)
  stay refresh-stable.

Fake items deliberately cover each exclusion class: low-volume, thin-spread,
over-max-margin, over-budget, and the three realism-filter reasons
(thin history / manipulated / single-order spread).

Throwaway — not for promotion. The winner gets rewritten against the real
ranking query and schema; see the `prototype` skill for the capture/cleanup convention.
