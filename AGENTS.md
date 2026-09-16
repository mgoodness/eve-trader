## Agent skills

### Issue tracker

Issues live in this repo's GitHub Issues, using the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Workflow

Every issue's work happens on a dedicated branch off the default branch, and is delivered as a pull request: create the branch before writing code, commit to it, push it, and open a PR against the default branch when the work is done. Each PR description must use GitHub closing keywords (for example, `Closes #15`) for every implemented issue so the merged PR closes its issue(s). After the PR merges, delete the local and remote feature branch and return to the default branch.
