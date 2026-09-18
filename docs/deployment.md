# Production deployment

`terraform apply` produces a fully-configured VM: it installs Docker, sqlite3,
and Caddy; creates the swap file and the `/var/lib/eve-trader` state directory;
provisions the `deploy` user with its authorized key and scoped sudoers; writes
the Caddy config and the non-secret app env; and generates the app secrets on
the VM at first boot. No manual SSH bootstrap is required (see
[`infra/startup.sh`](../infra/startup.sh) and [`infra/README.md`](../infra/README.md)).

The operator scripts (`eve-trader-start`, `-deploy`, `-secrets`, `-backup`, and
the backup cron) version with the application, so they are installed by the
deploy and secrets workflows rather than by `terraform apply` — each run scps
them to the VM and installs them via the root-owned
`eve-trader-install-scripts` helper. Caddy proxies the public hostname to
`127.0.0.1:8080` with a publicly trusted certificate; the workflow deliberately
uses normal HTTPS certificate verification.

What still needs a human before the first deploy: create the DNS A record
([Domain, TLS, and redirect URIs](#domain-tls-and-redirect-uris)), configure the
GitHub `production` environment ([GitHub setup](#github-setup)), and — only if
you make the GHCR package private — log the VM's Docker daemon into the
registry (below). Then run the deploy and secret-provisioning workflows.

## Terraform inputs for the bootstrap

The startup script reads its non-secret config from instance metadata, so
`terraform apply` needs three additional variables (see
[`infra/README.md`](../infra/README.md)):

| Variable            | What it is                                                              |
|---------------------|------------------------------------------------------------------------|
| `domain`            | Public hostname (see [Domain, TLS, and redirect URIs](#domain-tls-and-redirect-uris)). |
| `esi_client_id`     | EVE developer app client ID for the production app.                     |
| `deploy_public_key` | SSH public key for the `deploy` user; its private half is the GitHub `PRODUCTION_SSH_PRIVATE_KEY` secret. |

The packaged Caddy unit does **not** read `/etc/default/caddy`, so the startup
script installs `caddy-environment.conf` as a systemd drop-in that loads it.
Without the drop-in, `{$EVE_TRADER_DOMAIN}` expands empty and Caddy fails with
`unrecognized global option`.

The app secrets (`EVE_TRADER_COOKIE_SECRET`, `EVE_TRADER_TOKEN_KEY`) are
generated on the VM at first boot into `/var/lib/eve-trader/secrets` (guarded so
a re-boot never rotates them) and never enter Terraform state. Run **Provision
production secrets** only to rotate or override them ([Secrets](#secrets)).

## Registry credentials (private package only)

This repo's GHCR package is anonymously pullable, so `eve-trader-deploy` needs no registry credentials. If you make the package private, log the VM's Docker daemon in once with a read-only (`read:packages`) token:

```sh
echo "$TOKEN" | sudo docker login ghcr.io -u "$GITHUB_USER" --password-stdin
```

`/var/lib/eve-trader` holds the SQLite database (`eve-trader.db`), the `secrets` env-file, the non-secret runtime `env` file, `deployments.log`, `current-image`/`previous-image`, and `backups/` (see [Backups](#backups)).

## Domain, TLS, and redirect URIs

The public hostname is an operator-supplied value, passed to `terraform apply` as the `domain` variable (it also feeds `deploy_public_key`'s sibling metadata). Point an `A` record at the VM's external static IP (output by `terraform apply`); the startup script writes `EVE_TRADER_DOMAIN` for Caddy, and Caddy obtains/renews a Let's Encrypt certificate automatically and proxies HTTPS to `127.0.0.1:8080`.

An EVE developer application accepts a **single** callback URL, so register the production URI on the production app:

| Environment | Redirect URI                              |
|-------------|-------------------------------------------|
| Production  | `https://<domain>/auth/callback`          |

Register the local-dev URI (`http://localhost:<port>/auth/callback`) on a **separate** developer application with its own client ID, or temporarily change the production app's callback URL while developing locally. The app takes the callback from `EVE_TRADER_CALLBACK_URL` and the client ID from `EVE_TRADER_ESI_CLIENT_ID`; the startup script derives both from the `domain` and `esi_client_id` variables and writes them to `/var/lib/eve-trader/env`, and `eve-trader-start` passes that file to the container when present (recreating the container so a change takes effect immediately).

## Logging

The app writes structured JSON lines to stdout; Docker's default `json-file` driver captures them and they remain viewable with `docker logs eve-trader` over SSH.

In addition, the **Google Cloud Ops Agent** runs on the VM and ships the `eve-trader` container's `json-file` stdout to **GCP Cloud Logging**, so logs are queryable in Logs Explorer without shelling into the VM. The agent tails the existing Docker logs (it does not change the container's logging driver, so `docker logs` is unaffected) with a custom, app-scoped config (written by `infra/startup.sh` to `/etc/google-cloud-ops-agent/config.yaml`) that unwraps the Docker envelope and then the app's `slog` JSON so the structured fields are preserved as filterable payload — filter in Logs Explorer by fields like `severity` and `msg` rather than searching a flattened text blob.

Setup is codified, not manual (see [`docs/adr/0003`](https://github.com/mgoodness/eve-trader/blob/agent-context/docs/adr/0003-cloud-logging-for-app-container.md)):

- **Terraform** (`infra/main.tf`) creates a dedicated, least-privilege service account granted only `roles/logging.logWriter`, and binds it to the instance with just the `logging.write` OAuth scope. The instance no longer uses the default compute service account.
- **`infra/startup.sh`** installs and starts the Ops Agent on every boot, idempotently (a no-op once installed), alongside the swap file.

Only the `eve-trader` app container's logs are shipped. Caddy runs as a native systemd service (not a container) so it is not picked up, and the config's explicit `default_pipeline` overrides the agent's built-in one so host system/SSH (`syslog`) logs are **not** forwarded either. No exclusion filters, custom retention, or custom log buckets are configured — GCP defaults apply (50 GiB/month free ingestion, 30-day default retention), which comfortably covers this single-user tool's volume. No app secrets (`EVE_TRADER_*` values) are ever logged, so none reach Cloud Logging (see `docs/spec/v1.md` §8).

## GitHub setup

1. Enable GitHub Container Registry for the repository. Successful pushes to `main` run CI and publish the immutable image `ghcr.io/<owner>/<repo>:candidate-<full-commit-sha>`. That tag is the deployment target; the deploy workflow can resolve it for you (see [Deploy and rollback](#deploy-and-rollback)).
2. Create a protected Actions environment named `production` with required reviewers. Add the deployment SSH private key and VM host as environment secrets `PRODUCTION_SSH_PRIVATE_KEY` and `PRODUCTION_VM_HOST`; add `PRODUCTION_VM_USER` and `PRODUCTION_URL` as environment variables.
3. Do not put registry credentials, EVE secrets, or any other secret in the Docker image.

## Deploy and rollback

Run **Deploy production** manually. Leave the `image` input blank to deploy the immutable `candidate-<sha>` built from the current `main` HEAD — the workflow resolves that tag from the `main` commit and verifies it exists in GHCR before proceeding, failing with a clear message if CI has not finished publishing it. Provide an explicit `candidate-<sha>` tag to deploy a specific candidate instead (required when running from any ref other than `main`). The protected environment pauses the job for reviewer approval; the `production` concurrency group serializes deployments. The SSH/VM steps are credentials for the deployment key; nothing is logged.

The remote **deploy** script:

1. Records the currently active image as `previous-image`, then pulls the candidate *before* stopping anything — a pull failure leaves the active container untouched and fails the run.
2. Takes a consistent pre-switch SQLite backup (`sqlite3 .backup`) into `backups/`, keeping the newest 5.
3. Starts the candidate through **start**; if `docker run` fails, restores the previous image and exits non-zero.
4. Probes `http://127.0.0.1:8080/healthz` locally; on failure, dumps the container log, restores the previous image, and exits non-zero.
5. On success, logs the deploy, records `current-image`, prunes this repo's images not among the last 5 deployed and not in use (the fixed rollback set), and prints `deployed=<image>`.

The workflow then polls the Caddy HTTPS URL for `/healthz` and `/` for up to three minutes. On success it records `image, approver, timestamp, target VM, health-check=passed` in the Actions log. If the public check fails it records `health-check=failed` with the same fields, restores the previous image via a second deploy call, and fails the job.

A rollback is another workflow run using the `rollback` input with a retained `candidate-<sha>` tag; it substitutes that tag on the same registry and never touches SQLite. Image rollback is in-place; database restoration is a separate operator-controlled operation from the pre-switch backups.

GitHub Actions status is the v1 failure notification; no external alerting is required.

## Secrets

Run **Provision production secrets** separately, supplying `EVE_TRADER_COOKIE_SECRET` and `EVE_TRADER_TOKEN_KEY`. Values travel base64 over SSH, are never echoed, and are stored only in `/var/lib/eve-trader/secrets` (mode `0600`), which is passed as the container's `--env-file`; rotation recreates the container from the currently deployed image so new values take effect immediately. Secrets are never baked into the image.

## Backups

Two independent rotating sets live under `/var/lib/eve-trader/backups/`:

- **Daily:** `/etc/cron.d/eve-trader-backup` runs `/usr/local/sbin/eve-trader-backup` at 03:17 UTC every day. It takes a consistent `sqlite3 .backup` snapshot to `backups/daily/eve-trader-<YYYY-MM-DD>.db` and prunes to the newest **7** daily copies. There is no off-VM destination for v1 — VM loss is an accepted risk (see `docs/spec/v1.md` §8).
- **Pre-switch:** the deploy script takes a consistent backup to `backups/eve-trader-<UTC timestamp>.db` before switching images and keeps the newest **5**. This is what covers a schema-migrating release that must be rolled back in place.

A daily snapshot can be verified by running `sudo /usr/local/sbin/eve-trader-backup` and listing `backups/daily/`.

## Migrations and backups

Schema changes ship as backward-compatible migrations applied by the app at startup (the schema is idempotent `CREATE/ALTER` statements on every `Open`) — a release must not make the prior application version unable to run against the existing database. Before any schema change, the deploy script's automatic pre-switch backup covers rollback of the application. Restore from a backup rather than editing the live database, and test rollback against the migrated database before deploying:

```sh
docker stop eve-trader
cp /var/lib/eve-trader/backups/daily/eve-trader-<date>.db /var/lib/eve-trader/eve-trader.db
sudo /usr/local/sbin/eve-trader-start "$(cat /var/lib/eve-trader/current-image)"
```

## End-to-end verification

This milestone's final acceptance is a manual pass against the real deployment. After the first successful **Deploy production** run and secret provisioning, confirm:

1. Visiting `https://<domain>/` shows the **Re-authenticate with EVE** banner on first boot (no token stored yet) and presents a valid, publicly trusted certificate.
2. Following `/auth/login` completes the round trip through real EVE SSO and returns to `https://<domain>/auth/callback` without an `invalid redirect_uri` error.
3. After authentication, the opportunity table populates from live ESI data (a 5-minute order-book poll plus the daily history/skill refreshes), and the re-auth banner is gone.
4. `docker logs eve-trader` shows structured JSON lines for the pollers and the login.
5. `swapon --show` reports `/swapfile`, and `ls /var/lib/eve-trader/backups/daily/` contains a snapshot after the next 03:17 UTC run.
