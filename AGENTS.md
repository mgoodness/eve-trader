# AGENTS.md

`eve-trader` is a station trading recommendation engine for EVE Online: it
reads live market data and the pilot's character skills, then recommends buy
and sell order prices that still clear a target net margin.

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues on mgoodness/eve-trader via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
