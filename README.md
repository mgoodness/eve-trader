# eve-trader

A personal, single-character web app for [EVE Online](https://www.eveonline.com/) station trading, built against CCP's [ESI API](https://esi.evetech.net/ui/). It does two things:

- **Order tracking** — pulls your live market orders and shows them on a dashboard, with proactive Alerts (Undercut, Expiring, Price-Moved) surfaced in-app and via Discord, so you find out about a problem order without having to go looking for it.
- **Opportunity Scanner** — ranks items by net station-trading Margin and liquidity, independently at Jita and Rens, using ESI as the only data source.

See [`CONTEXT.md`](CONTEXT.md) for the precise domain vocabulary (Hub, Margin, Fees, Opportunity, Item Universe, Volume Window, Order, Alert, ...) and [`docs/adr/`](docs/adr/) for the reasoning behind non-obvious decisions.

## Status

This is a personal tool built for one character, not a general-purpose product — it supports exactly one authenticated character at a time (see [ADR-0003](docs/adr/0003-refresh-token-in-sqlite.md)) and covers exactly two hubs (Jita and Rens). It's not designed for other people to log into a shared instance; self-hosting your own instance is the intended way to use it.

## Stack

- Go, `net/http` stdlib routing (no router library), `html/template` for server-rendered pages
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite driver, no CGO (see [ADR-0002](docs/adr/0002-pure-go-sqlite-driver.md))
- No frontend build step, no SPA framework

## Running it

```sh
go build ./cmd/eve-trader
EVE_TRADER_ESI_CLIENT_ID=... EVE_TRADER_ESI_CLIENT_SECRET=... ./eve-trader
```

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `EVE_TRADER_ESI_CLIENT_ID` | yes | — | EVE SSO application Client ID ([developers.eveonline.com](https://developers.eveonline.com/)) |
| `EVE_TRADER_ESI_CLIENT_SECRET` | yes | — | EVE SSO application Client Secret |
| `EVE_TRADER_DISCORD_WEBHOOK_URL` | no | unset | Discord webhook for Alert delivery; leaving it unset disables Discord delivery (in-app Alerts still work) |
| `EVE_TRADER_ADDR` | no | `:8080` | HTTP listen address |
| `EVE_TRADER_DB_PATH` | no | `eve-trader.db` | SQLite database file path |
| `EVE_TRADER_CALLBACK_URL` | no | `https://eve-trader.opsgoodness.net/auth/callback` | OAuth callback URL registered with the SSO application |
| `EVE_TRADER_DASHBOARD_URL` | no | `https://eve-trader.opsgoodness.net/` | Base URL used in Discord Alert links back to the dashboard |

## Development

```sh
go build ./...
go vet ./...
go test -race ./...
```

`main` is branch-protected — changes land via pull request (CI must pass; no direct pushes, including from admins). See [`docs/agents/issue-tracker.md`](docs/agents/issue-tracker.md) for how issues are tracked and triaged.

## Deployment

Every push to `main` deploys automatically (see [ADR-0008](docs/adr/0008-continuous-deploy-without-release-please.md)): [GoReleaser](https://goreleaser.com/) builds and pushes a `linux/amd64` Docker image to `ghcr.io/mgoodness/eve-trader`, tagged both the immutable commit SHA and a floating `stable` tag. A [Watchtower](https://github.com/containrrr/watchtower) instance on the host polls `stable` and redeploys automatically — no manual steps, no version tags, no inbound trigger or SSH secret into the host. See [ADR-0004](docs/adr/0004-caddy-over-gcp-native-tls.md) for the TLS/reverse-proxy setup.
