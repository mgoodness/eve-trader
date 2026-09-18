# eve-trader

A single-user, hosted web tool that scans Rens's own order book for
profitable station-trading opportunities. See
[`docs/spec/v1.md`](https://github.com/mgoodness/eve-trader/blob/agent-context/docs/spec/v1.md) for the full build spec.

## Development

Toolchain versions (Go, Terraform) are pinned in `mise.toml`:

```sh
mise install
```

Build and run:

```sh
go build -o eve-trader .
EVE_TRADER_DB_PATH=eve-trader.db EVE_TRADER_ADDR=:8080 ./eve-trader
```

The one-time EVE SSO login (`/auth/login` -> `/auth/callback`, see
[`docs/spec/v1.md` §6](https://github.com/mgoodness/eve-trader/blob/agent-context/docs/spec/v1.md#6-oauth--esi-auth-flow)) reads its
settings from environment variables:

| Variable                   | Purpose                                                                       |
|-----------------------------|--------------------------------------------------------------------------------|
| `EVE_TRADER_ESI_CLIENT_ID`  | The EVE developer app's client ID.                                            |
| `EVE_TRADER_CALLBACK_URL`   | The registered redirect URI (default `http://localhost:8080/auth/callback`).  |
| `EVE_TRADER_COOKIE_SECRET`  | Signs the short-lived PKCE cookie. Any string; never commit it.               |
| `EVE_TRADER_TOKEN_KEY`      | Encrypts the persisted refresh token at rest. Any string; never commit it.    |

An EVE developer application accepts a single callback URL. Register the
production URI `https://<domain>/auth/callback`; use a separate developer
application (with its own client ID) for local dev, or temporarily change the
app's callback URL. The value is set through `EVE_TRADER_CALLBACK_URL`; see
[`docs/deployment.md`](docs/deployment.md#domain-tls-and-redirect-uris) for
the full production setup.

If `EVE_TRADER_COOKIE_SECRET`/`EVE_TRADER_TOKEN_KEY` are unset, the app
generates ephemeral secrets for that process only -- fine for local
development, but in-flight logins and previously stored tokens won't
survive a restart.

Run tests:

```sh
go test ./...
```

## Infrastructure

The hosting environment (GCP VM, disk, static IP, firewall) is Terraform
infrastructure-as-code. See [`infra/README.md`](infra/README.md) to
(re)create or tear it down.
