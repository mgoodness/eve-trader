# Infrastructure

Deploys the eve-trader hosting environment to GCP — the VM, disk, static IP,
and firewall that the app runs behind — as Terraform infrastructure-as-code
pinned to Terraform 1.16.2 ([`mise.toml`](../mise.toml)) using the
[Google provider](https://registry.terraform.io/providers/hashicorp/google/latest).

The config provisions the infrastructure **and** configures the VM: the
instance startup script ([`startup.sh`](startup.sh)) folds the whole OS
bootstrap into `terraform apply`, so a fresh apply yields a VM ready for the
first deploy with no manual SSH (see the split below and
[`docs/deployment.md`](../docs/deployment.md)).

The Caddy config files — [`Caddyfile`](Caddyfile) and
[`caddy-environment.conf`](caddy-environment.conf) — are passed to the VM as
instance metadata and installed by the startup script. The operator scripts —
[`eve-trader-start`](eve-trader-start), [`eve-trader-deploy`](eve-trader-deploy),
[`eve-trader-secrets`](eve-trader-secrets), [`eve-trader-backup`](eve-trader-backup),
and [`eve-trader-backup.cron`](eve-trader-backup.cron) — version with the
application, so the deploy/secrets workflows install them (via the startup
script's `eve-trader-install-scripts` helper), not `terraform apply`.

### Config vs. bootstrap split

- **Terraform / startup script owns infra + config:** OS packages (Docker,
  sqlite3, Caddy + repo), swap, the `/var/lib/eve-trader` state directory
  (uid 65532), the `deploy` user + authorized key + scoped sudoers, Caddy
  config + systemd drop-in, the non-secret app env (`domain`, `esi_client_id`),
  and first-boot generation of the app secrets.
- **Deploy/secrets workflows own the operator scripts** because they version
  with the application; installing them here would force a `terraform apply` on
  every edit.
- **Secrets never enter Terraform state.** `EVE_TRADER_COOKIE_SECRET` and
  `EVE_TRADER_TOKEN_KEY` are generated on the VM at first boot (guarded so a
  re-boot never rotates them). Non-secret values (`domain`, `esi_client_id`,
  `deploy_public_key`, the Caddy files) do pass through metadata and state,
  which is fine.

Still external to Terraform: the DNS A record, the GitHub `production`
environment/secrets, the GHCR token (private package only), and the app image
pull/run handled by the deploy pipeline (see
[`docs/deployment.md`](../docs/deployment.md)).

## What it provisions

- One Always Free `e2-micro` instance in an Always Free region
  (`us-west1`, `us-central1`, or `us-east1`).
- A `pd-standard` boot disk, ≤ 30 GB.
- An external static IPv4 address.
- A firewall rule allowing inbound HTTPS (443) only.
- A 2 GB swap file, created on every boot by the instance startup script
  [`startup.sh`](startup.sh) as an OOM safety net for the 1 GB `e2-micro`
  (see [`docs/spec/v1.md` §8](../docs/spec/v1.md)).

And, via [`startup.sh`](startup.sh) on every boot (idempotently):

- Docker, sqlite3, and Caddy (plus Caddy's package repo).
- The `/var/lib/eve-trader` runtime state directory, owned by uid 65532 (the
  distroless nonroot user the app runs as).
- The `deploy` user with its authorized SSH key, scoped sudoers, and the
  root-owned `eve-trader-install-scripts` helper the deploy workflow uses.
- Caddy config (Caddyfile, systemd drop-in, `EVE_TRADER_DOMAIN`) and the
  non-secret app env.
- First-boot generation of the app secrets, kept out of Terraform state.
- The Google Cloud Ops Agent (see [`docs/adr/0003`](../docs/adr/0003-cloud-logging-for-app-container.md)).

### Firewall scope

The HTTPS-only rule is created on the project's `default` VPC, tagged to the
instance. GCP's default network ships with its own pre-existing ingress rules
(internal, SSH, RDP); this config does not remove those. A truly default-deny
VPC requires managing/removing those rules or using a project dedicated to
this deployment.

## Installing Terraform

The repo pins Terraform via `mise` (see [`mise.toml`](../mise.toml)). Install
the pinned version once:

```sh
mise install
```

Then run Terraform through `mise` so the pinned version is used
(`mise x` runs the command in the toolchain's PATH):

```sh
cd infra
mise x -- terraform version   # prints 1.16.2
mise x -- terraform init
```

Do **not** install Terraform with Homebrew, apt, or a manual download — `mise`
is the toolchain manager.

## (Re)creating the environment

The standard way to create the environment from scratch (also the way to
teardown after changes):

```sh
cd infra
mise x -- terraform init        # one-time; downloads the pinned provider
mise x -- terraform plan -out=tfplan
mise x -- terraform apply tfplan
```

To tear it down cleanly:

```sh
cd infra
mise x -- terraform destroy
```

(`terraform init` is included in the from-scratch flow so a fresh checkout
has everything it needs; running it again later is a no-op.)

## Variables

| Variable       | Default        | Notes                                      |
|----------------|----------------|--------------------------------------------|
| `project_id`   | *(required)*   | GCP project ID.                            |
| `region`       | `us-central1`  | Restricted to Always Free regions.         |
| `zone`         | `us-central1-a`| Must be in the selected region.            |
| `name`         | `eve-trader`   | Name prefix for created resources.         |
| `machine_type` | `e2-micro`     | Always Free tier. Set to nothing else.     |
| `domain`          | *(required)* | Public hostname; Caddy gets a Let's Encrypt cert for it. |
| `esi_client_id`   | *(required)* | EVE developer app client ID for production. |
| `deploy_public_key` | *(required)* | SSH public key for the `deploy` user. |

`domain`, `esi_client_id`, and `deploy_public_key` are non-secret and are
passed to the VM as instance metadata (and thus land in Terraform state, which
is fine). The app secrets are generated on the VM and never enter Terraform.

Example:

```sh
mise x -- terraform plan \
  -var project_id=my-project \
  -var region=us-east1 \
  -var zone=us-east1-b \
  -var domain=trader.example.com \
  -var esi_client_id=abcd1234 \
  -var deploy_public_key="$(cat ~/.ssh/eve-trader-deploy.pub)" \
  -out=tfplan
```

## State handling

Terraform state lives in `infra/terraform.tfstate` (a local backend), written
by the single operator who runs `apply`. State, plan, and variable files are
excluded from git via [`infra/.gitignore`](.gitignore); the compiled `.terraform/`
provider cache is too. The provider version — `hashicorp/google` — is pinned in
`.terraform.lock.hcl` (committed), so any operator gets the same provider
version when they run `terraform init`.

Because state is local, only one machine/operator can manage the environment
at a time. **Never commit the state file.** The provider version is pinned
separately via the committed `.terraform.lock.hcl` (see above). If a second
operator needs to manage the same deployment, do not simply re-run
`terraform apply` from a second machine: both would hold competing state
files. Options are:

1. Copy the existing state file to the new machine before running anything
   local (`terraform apply` re-uploads it), **or**
2. Migrate to a remote backend (e.g. GCS) so both machines share one state
   with locking.
