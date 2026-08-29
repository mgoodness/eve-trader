## Agent skills

### Issue tracker

Issues live as GitHub issues in [mgoodness/eve-trader](https://github.com/mgoodness/eve-trader) (uses the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the default label vocabulary — each canonical role's label string equals its name (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout — `CONTEXT.md` + `docs/adr/` at the repo root (created lazily as concepts are resolved). See `docs/agents/domain.md`.

### Branch protection

`main` is protected: direct pushes are rejected (`enforce_admins: true`), and merging requires the `test` status check (CI) to pass. No approving review is required (solo project), but the merge itself must go through a pull request. This means the implement → commit → push-to-main flow no longer works — instead: branch, push the branch, `gh pr create`, wait for CI, then `gh pr merge` (squash or merge, your call) once it's green.
