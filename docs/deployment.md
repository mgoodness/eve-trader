# Production deployment

The VM created by Terraform is only infrastructure. Before the first deploy, bootstrap a Debian VM with Docker, Caddy, and sqlite3, then install the three operator scripts in `infra/` as root-owned `0755` files on the VM: `eve-trader-start`, `eve-trader-deploy`, and `eve-trader-secrets` (paths below). Configure Caddy to proxy the public hostname to `127.0.0.1:8080` with a publicly trusted certificate; the workflow deliberately uses normal HTTPS certificate verification.

## VM bootstrap

```sh
sudo apt-get update && sudo apt-get install -y docker.io caddy sqlite3
sudo install -d -o 65534 -g 65534 /var/lib/eve-trader   # container uid, runtime state
sudo install -m 0755 eve-trader-start  /usr/local/sbin/eve-trader-start
sudo install -m 0755 eve-trader-deploy /usr/local/sbin/eve-trader-deploy
sudo install -m 0755 eve-trader-secrets /usr/local/sbin/eve-trader-secrets
```

Log the VM's Docker daemon into GHCR with a read-only token so `eve-trader-deploy` can pull candidates.

Give the deployment SSH user narrowly scoped sudo so the workflows can run only the operations they need:

```
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-start
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-deploy
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-secrets
deploy ALL=(root) NOPASSWD: /usr/bin/cat /var/lib/eve-trader/previous-image
```

`/var/lib/eve-trader` holds the SQLite database (`eve-trader.db`), the `secrets` env-file, `deployments.log`, `current-image`/`previous-image`, and the rotating `backups/` directory.

## GitHub setup

1. Enable GitHub Container Registry for the repository. Successful pushes to `main` run CI and publish the immutable image `ghcr.io/<owner>/<repo>:candidate-<full-commit-sha>`. That tag is the deployment input.
2. Create a protected Actions environment named `production` with required reviewers. Add the deployment SSH private key and VM host as environment secrets `PRODUCTION_SSH_PRIVATE_KEY` and `PRODUCTION_VM_HOST`; add `PRODUCTION_VM_USER` and `PRODUCTION_URL` as environment variables.
3. Do not put registry credentials, EVE secrets, or any other secret in the Docker image.

## Deploy and rollback

Run **Deploy production** manually with an immutable `candidate-<sha>` tag. The protected environment pauses the job for reviewer approval; the `production` concurrency group serializes deployments. The SSH/VM steps are credentials for the deployment key; nothing is logged.

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

## Migrations and backups

Schema changes ship as backward-compatible migrations applied by the app at startup (the schema is idempotent `CREATE/ALTER` statements on every `Open`) — a release must not make the prior application version unable to run against the existing database. Before any schema change, the deploy script's automatic pre-switch backup covers rollback of the application; keeping 5 days/5 deploys of backups also supports manual database restoration. Restore from a pre-switch backup rather than editing the live database, and test rollback against the migrated database before deploying.
