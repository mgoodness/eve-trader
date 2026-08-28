# Item Universe: a small verified starter list, not a computed top-N

The spec calls the Item Universe a "bounded top-N (~200) most-liquid items per Hub" — but computing *which* ~200 items are actually the most liquid would require first fetching volume data for the full ~14,000-item marketable catalog, which is exactly the full-universe scan the parent spec's Out of Scope section defers past v1 ("Full-universe Scanner coverage... a later refinement once the mechanics prove out"). There's no ESI endpoint that ranks items by liquidity directly.

We chose a small, hand-curated, individually-verified list (the seven base minerals, Morphite, and PLEX — nine items) over either (a) fabricating a plausible-looking 200-item list from memory, which risks silently feeding wrong type IDs into every Margin calculation, or (b) implementing dynamic top-N discovery now, which is explicitly out of scope. Every ID in the starter list was confirmed live against `GET /universe/types/{id}/` rather than recalled from training data.

This means the Item Universe is currently well short of "~200" in practice. That's a data-curation task, not a code change: `internal/scanner`'s `ItemUniverse` is a plain `[]int32`, extending it to more verified items doesn't touch the scanning, ranking, or filtering logic at all. Treat filling it out as ordinary backlog work, not a reason to revisit this decision.
