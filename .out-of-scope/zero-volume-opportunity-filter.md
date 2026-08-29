# Unconditional zero-volume Opportunity filter

This project does not add a dedicated, always-on floor that excludes Opportunities with exactly zero average daily volume from the ranked list, beyond the existing live, user-configurable `MinVolume` filter.

## Why this is out of scope

The original request (#50) was prompted by observing items with a genuine, live two-sided market showing `0` average daily volume on the dashboard -- most notably the Slasher, which live ESI market history showed had real, healthy trade volume at both Jita and Rens. That `0` wasn't real data; it was a caching bug (#55): `scanner.refreshStaleCache` gated volume refresh with a single per-Hub 24-hour timestamp, so an item newly added to `ItemUniverse` (e.g. by #36's expansion) could get its price populated quickly (5-minute order-book cache) while its volume cache row stayed unset -- silently read back as `0.0` -- for up to 24 hours, even though it had never actually been fetched.

#56 fixed that: any `ItemUniverse` item missing a cached volume value now gets backfilled on the very next Scan, independent of the Hub-wide 24h window. With that fixed, a `0` average daily volume now reliably means "no trade history at this Hub, ever" -- there's no longer a caching artifact masquerading as real data.

Once the underlying bug was gone, the original motivation for a dedicated always-on zero-volume floor went with it: a genuinely dead item (never traded) is rare in a hand-curated `ItemUniverse` (see ADR-0007's inclusion bar, which already requires confirmed liquidity at at least one Hub before an item is added at all), and the existing live `MinVolume` filter already lets a user exclude it at request time same as any other thin market. Adding a second, unconditional floor on top would be solving a problem that turned out not to exist.

## Prior requests

- #50: "Exclude items with 0 average daily volume" -- closed as wontfix once diagnosed as a symptom of #55, fixed by #56.
