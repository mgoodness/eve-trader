# Production deployment

The VM created by Terraform is only infrastructure. Before the first deploy, bootstrap a Debian VM with Docker, Caddy, and sqlite3, then install the operator scripts in `infra/` as root-owned `0755` files on the VM: `eve-trader-start`, `eve-trader-deploy`, `eve-trader-secrets`, and `eve-trader-backup` (paths below). Configure Caddy to proxy the public hostname to `127.0.0.1:8080` with a publicly trusted certificate; the workflow deliberately uses normal HTTPS certificate verification.

## VM bootstrap

Install Docker, sqlite3, and Caddy. Caddy is not in Debian's base repos, so add its official package repository first:

```sh
sudo apt-get update && sudo apt-get install -y docker.io sqlite3 curl debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt-get update && sudo apt-get install -y caddy
```

The Terraform startup script (`infra/startup.sh`) has already created a 2 GB swap file. Verify it and, if the VM predates that change, create it manually:

```sh
swapon --show          # expect /swapfile
# fallback if empty:
# sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
# echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

Install the operator scripts, the Caddy config, and the daily backup cron job:

```sh
sudo install -d -o 65532 -g 65532 /var/lib/eve-trader   # distroless nonroot uid, runtime state
sudo install -m 0755 eve-trader-start   /usr/local/sbin/eve-trader-start
sudo install -m 0755 eve-trader-deploy  /usr/local/sbin/eve-trader-deploy
sudo install -m 0755 eve-trader-secrets /usr/local/sbin/eve-trader-secrets
sudo install -m 0755 eve-trader-backup  /usr/local/sbin/eve-trader-backup
sudo install -m 0644 eve-trader-backup.cron /etc/cron.d/eve-trader-backup
sudo install -m 0644 Caddyfile /etc/caddy/Caddyfile
sudo install -D -m 0644 caddy-environment.conf /etc/systemd/system/caddy.service.d/override.conf
printf 'EVE_TRADER_DOMAIN=%s\n' "$DOMAIN" | sudo tee /etc/default/caddy
printf 'EVE_TRADER_CALLBACK_URL=https://%s/auth/callback\nEVE_TRADER_ESI_CLIENT_ID=%s\n' "$DOMAIN" "$CLIENT_ID" | sudo tee /var/lib/eve-trader/env
sudo systemctl daemon-reload
sudo systemctl restart caddy
```

`$DOMAIN` and `$CLIENT_ID` are the operator-supplied hostname and EVE developer app client ID (see [Domain, TLS, and redirect URIs](#domain-tls-and-redirect-uris)).

The packaged Caddy unit does **not** read `/etc/default/caddy`, so `caddy-environment.conf` is installed as a systemd drop-in that loads it. Without the drop-in, `{$EVE_TRADER_DOMAIN}` expands empty and Caddy fails with `unrecognized global option`. Alternatively, replace the placeholder in `/etc/caddy/Caddyfile` with the literal domain instead.

This repo's GHCR package is anonymously pullable, so `eve-trader-deploy` needs no registry credentials. If you make the package private, log the VM's Docker daemon in once with a read-only (`read:packages`) token:

```sh
echo "$TOKEN" | sudo docker login ghcr.io -u "$GITHUB_USER" --password-stdin
```

Give the deployment SSH user narrowly scoped sudo so the workflows can run only the operations they need:

```
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-start
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-deploy
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-secrets
deploy ALL=(root) NOPASSWD: /usr/bin/cat /var/lib/eve-trader/previous-image
```

`/var/lib/eve-trader` holds the SQLite database (`eve-trader.db`), the `secrets` env-file, the non-secret runtime `env` file, `deployments.log`, `current-image`/`previous-image`, and `backups/` (see [Backups](#backups)).

## Domain, TLS, and redirect URIs

The public hostname is an operator-supplied, deploy-time value. Point an `A` record at the VM's external static IP (output by `terraform apply`), set `EVE_TRADER_DOMAIN` for Caddy as above, and Caddy obtains/renews a Let's Encrypt certificate automatically and proxies HTTPS to `127.0.0.1:8080`.

An EVE developer application accepts a **single** callback URL, so register the production URI on the production app:

| Environment | Redirect URI                              |
|-------------|-------------------------------------------|
| Production  | `https://<domain>/auth/callback`          |

Register the local-dev URI (`http://localhost:<port>/auth/callback`) on a **separate** developer application with its own client ID, or temporarily change the production app's callback URL while developing locally. The app takes the callback from `EVE_TRADER_CALLBACK_URL` and the client ID from `EVE_TRADER_ESI_CLIENT_ID`; production bootstrap writes both to `/var/lib/eve-trader/env`, and `eve-trader-start` passes that file to the container when present (recreating the container so a change takes effect immediately).

## Logging

The app writes structured JSON lines to stdout; Docker's default `json-file` driver captures them and they are viewable with `docker logs eve-trader`. No external log aggregation service is used, and the app runs no sidecars (see `docs/spec/v1.md` §8).

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
