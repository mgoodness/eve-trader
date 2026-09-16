# Infrastructure

Deploys the eve-trader hosting environment to GCP — the VM, disk, static IP,
and firewall that the app runs behind — as Terraform infrastructure-as-code
pinned to Terraform 1.16.2 ([`mise.toml`](../mise.toml)) using the
[Google provider](https://registry.terraform.io/providers/hashicorp/google/latest).

The config provisions infrastructure only; application deployment (binary, TLS, service) is
covered separately by [`docs/deployment.md`](../docs/deployment.md).

## What it provisions

- One Always Free `e2-micro` instance in an Always Free region
  (`us-west1`, `us-central1`, or `us-east1`).
- A `pd-standard` boot disk, ≤ 30 GB.
- An external static IPv4 address.
- A firewall rule allowing inbound HTTPS (443) only.

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

Example:

```sh
mise x -- terraform plan \
  -var project_id=my-project \
  -var region=us-east1 \
  -var zone=us-east1-b \
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
