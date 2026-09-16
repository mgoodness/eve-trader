# Production deployment

The VM created by Terraform is only infrastructure. Before the first deploy, bootstrap a Debian VM with Docker, Caddy, and the two scripts in `infra/` installed as root-owned `0755` files at `/usr/local/sbin/eve-trader-deploy` and `/usr/local/sbin/eve-trader-secrets`. Create `/var/lib/eve-trader`, owned by the deployment user, and configure Caddy to proxy the public hostname to `127.0.0.1:8080`. Caddy must obtain a publicly trusted certificate; the workflow deliberately uses normal HTTPS certificate verification.

## GitHub setup

1. Enable GitHub Container Registry for the repository. Successful pushes to `main` run CI and publish `ghcr.io/<owner>/<repo>:candidate-<full-commit-sha>`. The SHA tag is immutable and is the deployment input; `candidate` is only a convenience pointer.
2. Create a protected Actions environment named `production` with required reviewers. Add the deployment SSH private key and VM host as environment secret `PRODUCTION_SSH_PRIVATE_KEY` and `PRODUCTION_VM_HOST`; add `PRODUCTION_VM_USER` and `PRODUCTION_URL` as environment variables. The SSH public key is installed in the VM user's `authorized_keys` and that user may run only the two deployment operations via narrowly scoped sudo rules.
3. Give the VM's Docker credential read-only access to the registry. Do not put registry credentials, EVE secrets, or any other secret in the Docker image.

## Deploy and rollback

Run **Deploy production** manually with an immutable `candidate-<sha>` tag. The protected environment pauses the job for reviewer approval. GitHub Actions serializes jobs with the `production` concurrency group. The remote script pulls before stopping the current container, then verifies local startup and restores the prior image if startup fails. The SQLite database is `/var/lib/eve-trader/eve-trader.db` mounted at `/data` and is never replaced during deployment.

The last fixed number of images is retained by the VM's Docker retention policy (bootstrap should retain **5** images, including the active one); remove older images only after confirming the deployment log. A rollback is another workflow run using a retained `candidate-<sha>` tag and the rollback input. It does not restore SQLite. Database recovery is a separate, operator-controlled operation from a pre-migration backup.

The workflow then polls the Caddy HTTPS URL for `/healthz` and `/` for up to three minutes, and records image, approver, UTC timestamp, target VM, and result in the Actions log. GitHub Actions status is the v1 failure notification.

## Secrets and migrations

Run **Provision production secrets** separately after approval, supplying the values interactively. Values are sent over SSH, never echoed, and stored only in `/var/lib/eve-trader/secrets` with mode `0600`; restart the service afterward. Rotate them through the same operation.

Any schema-changing release must first add a backward-compatible migration to the application and create a SQLite backup before changing the schema. The old image must remain able to open the existing database. Test rollback against the migrated database before deploying; restoring the database is intentionally not part of image rollback.
