# Fee computation: exact formulas, skill IDs, and Hub-owner standings

ADR-0001 already decided *that* Fees are computed live from the character's skills and standings rather than a fixed percentage. This records the exact formulas and EVE static-data IDs that decision depends on, since getting them wrong would silently corrupt every Margin figure and none of it is guessable from ESI's schema alone.

**Broker fee** (paid twice per round-trip — once placing the buy order, once placing the sell order): `3% − 0.3%×BrokerRelationsLevel − 0.03%×FactionStanding − 0.02%×CorporationStanding`, floored at 0. Faction/Corporation standing here is specifically the character's *unmodified* standing with the Hub's station owner (Broker Relations does not benefit from standing-boosting skills like Connections) — so this is Hub-specific, not a single global rate. Confirmed against [EVE University's Tax page](https://wiki.eveuniversity.org/Tax).

**Sales tax** (paid once, on the sale): `7.5% × (1 − 0.11×AccountingLevel)`, floored at 0. Same source.

**Skill type IDs**: Accounting = `16622`, Broker Relations = `3446` — confirmed live against `GET /universe/types/{id}/`. (The walking skeleton's fake `CharacterSkills` data originally used placeholder IDs `3446`/`3447` for these two skills, guessed before this ticket needed the real ones; `3446` turned out to actually be Broker Relations, not Accounting. Fixed here.)

**Hub-owner IDs** for the standings lookup, confirmed live against `GET /universe/stations/{id}/` and `GET /universe/factions/`: Jita's station is owned by Caldari Navy (corporation `1000035`), part of the Caldari State (faction `500001`); Rens's station is owned by Brutor Tribe (corporation `1000049`), part of the Minmatar Republic (faction `500002`).
