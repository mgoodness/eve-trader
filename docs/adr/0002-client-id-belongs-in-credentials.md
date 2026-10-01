# Client ID belongs in `credentials.json`, not `config.toml`

The ESI `client_id` is public — PKCE sends it and no secret — so it would sit
naturally in `config.toml` beside the other non-secret defaults. We store it in
`credentials.json` (mode 600) instead, written by `eve-trader login` on first
run.

The boundary being drawn is *auth state versus configuration*: `config.toml`
holds the trading defaults a user may tune (budget, margins, thresholds), while
`credentials.json` holds everything needed to authenticate. Keeping the client
id with the refresh token means one file to copy between machines and one place
to look when auth fails, and it keeps spec §13's "credentials file is the only
source of auth" literally true.

The rejected alternative was reading `client_id` from `config.toml` (or an env
var), which is the more conventional split but scatters auth across two files and
two merges the login flow would have to reconcile. The `client_secret` field is
read from an existing file only for backward compatibility; PKCE needs no secret.
