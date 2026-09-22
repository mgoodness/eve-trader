# Standings in the broker-fee rate

The v2 portfolio and opportunity ranking charged fees from skills alone,
making every figure a pessimistic upper bound for a character with positive
standings (docs/spec/v2.md §4.3). The standings fast-follow folds the
character's NPC standings into a single broker-fee rate `R_b`, used by the
opportunity ranking, the portfolio P/L engine, and the re-list formula
alike. ([#98](https://github.com/mgoodness/eve-trader/issues/98),
[#108](https://github.com/mgoodness/eve-trader/issues/108); research pinned
on `research/standings-broker-fees`.)

## Decision

- **Formula.** `R_b = 3% − 0.3%×BrokerRelations − 0.03%×factionStanding −
  0.02%×corpStanding`, taken from CCP's support article. The station-owner
  corporation and its faction apply **additively**; a missing standing is 0,
  so an unresolved owner degrades to the skills-only v2 baseline rather than
  erroring.
- **Station owner.** `GET /universe/stations/{id}` gives the owning
  corporation id; the standing toward that corporation is the
  `npc_corp` entry, and toward its faction the `faction` entry. Rens
  (`60004588`, exported as `esi.RensStationID`) is the only station in
  scope.
- **Faction source.** ESI's `GET /corporations/{id}` omits `faction_id` for
  every NPC corporation except the four faction-warfare militia corps, so
  the corporation→faction mapping is **vendored** in
  `internal/npcfactions`, generated from the SDE `npcCorporations.jsonl`
  `factionID` field by `go run ./internal/npcfactions/generate
  <npcCorporations.jsonl>`. No runtime SDE dependency.
- **One rate everywhere.** `fees.BrokerFeeRate` takes the corp and faction
  standings and is the only broker-fee formula; the ranking
  (`ranking.LoadFeeRates`), the portfolio (`ledger.ComputePnL`), and the
  skills/standings-derived minimum-margin default all derive from it. The
  rate changes the same `max(100 ISK, order_value × R_b)` floor and the
  in-place re-list formula — standings move the rate, not the floor.
- **Fetch and consent.** Standings are fetched alongside skills by the
  shared `refreshTradingProfile` path (login callback and daily
  `SkillPoller`), ~1 h ESI cache, stored in `character_standing`; the owner
  is stored in `station_owner`. The new
  `esi-characters.read_standings.v1` scope requires one more re-consent; a
  token that refreshes but lacks it gets a 403 from the standings route,
  which latches the existing "Re-authenticate with EVE" banner.

## Open verification

The ESI OpenAPI spec does **not** state whether
`GET /characters/{id}/standings/` returns the *unmodified* standing (the
value the fee formula requires) or the Connections/Diplomacy-modified
effective standing. CCP's fee rule requires the unmodified value and
third-party tools treat the endpoint as such, but that is an inference, not
a cited primary claim. Verify by comparing the endpoint value for a test
character against their in-game base vs. effective standing. Until then,
`fees.BrokerFeeRate` assumes the value it is handed is already unmodified.
