# eve-trader

A single-user, hosted web tool that scans Rens's own order book for
profitable station-trading opportunities. See
[`docs/spec/v1.md`](docs/spec/v1.md) for the full build spec.

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

Run tests:

```sh
go test ./...
```
