#!/bin/sh
# GCE instance startup script, run on every boot. Folds the whole OS bootstrap
# into Terraform so `terraform apply` alone yields a configured VM with no
# manual SSH steps (see docs/deployment.md, infra/README.md). Every step is
# idempotent: safe to re-run and reuses existing state rather than recreating
# it.
#   1. OS packages: Docker, sqlite3, and Caddy (+ Caddy's package repo).
#   2. A 2 GB swap file as an OOM safety net for the 1 GB e2-micro.
#   3. The runtime state directory /var/lib/eve-trader (uid 65532).
#   4. The `deploy` user, its authorized key, scoped sudoers, and the fixed
#      helper the deploy workflow uses to install the operator scripts.
#   5. Caddy config (Caddyfile, systemd drop-in, non-secret env).
#   6. First-boot generation of the app secrets, kept out of Terraform state.
#   7. The Google Cloud Ops Agent, which ships the eve-trader container's
#      json-file stdout to Cloud Logging (docs/spec/v1.md §8, docs/adr/0003).
#
# Non-secret config values (domain, ESI client ID, deploy public key, and the
# Caddy config files) are read from instance metadata, set by main.tf. Secrets
# are generated here on the VM and never enter metadata or Terraform state.
set -eu

export DEBIAN_FRONTEND=noninteractive

# Read an instance-metadata attribute set by main.tf. Values are non-secret.
metadata() {
    curl -fsS -H 'Metadata-Flavor: Google' \
        "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1"
}

# True when the named apt package is not installed.
need_pkg() {
    ! dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'install ok installed'
}

# Install stdin to the given path with MODE only when the content differs, so a
# re-run on an unchanged host is a no-op. Returns 0 when the file changed, 1
# otherwise.
write_if_changed() {
    _path=$1
    _mode=$2
    _tmp=$(mktemp)
    cat >"$_tmp"
    if [ -f "$_path" ] && cmp -s "$_tmp" "$_path"; then
        rm -f "$_tmp"
        return 1
    fi
    install -m "$_mode" "$_tmp" "$_path"
    rm -f "$_tmp"
    return 0
}

# --- OS packages ----------------------------------------------------------
# Docker, sqlite3, and Caddy. Caddy is not in Debian's base repos, so its
# Cloudsmith package repo is added first. Gated on package presence so the
# steady-state boot does no network work (apt is network-dependent; retries
# guard the first install).
if need_pkg docker.io || need_pkg sqlite3 || need_pkg caddy; then
    apt-get update -o Acquire::Retries=3
    apt-get install -y -o Acquire::Retries=3 \
        ca-certificates curl gnupg debian-keyring debian-archive-keyring apt-transport-https
    if [ ! -f /usr/share/keyrings/caddy-stable-archive-keyring.gpg ]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' |
            gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    fi
    if [ ! -f /etc/apt/sources.list.d/caddy-stable.list ]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
            >/etc/apt/sources.list.d/caddy-stable.list
    fi
    apt-get update -o Acquire::Retries=3
    apt-get install -y -o Acquire::Retries=3 docker.io sqlite3 caddy
fi

# --- Swap file ------------------------------------------------------------
file=/swapfile

if ! swapon --show=NAME --noheadings | grep -qx "$file"; then
    if [ ! -f "$file" ]; then
        fallocate -l 2G "$file" || dd if=/dev/zero of="$file" bs=1M count=2048
        chmod 600 "$file"
        mkswap "$file"
    fi
    swapon "$file"
    grep -qE "^${file}[[:space:]]" /etc/fstab ||
        printf '%s none swap sw 0 0\n' "$file" >>/etc/fstab
fi

# --- Runtime state directory ---------------------------------------------
# Owned by uid 65532 (the distroless nonroot user the app runs as) so the
# container can write the SQLite database. `install -d` ensures existence and
# ownership without recreating or wiping an existing directory.
install -d -o 65532 -g 65532 /var/lib/eve-trader

# --- deploy user, sudoers, operator-script installer ----------------------
# The `deploy` user is the SSH identity the deploy/secrets workflows use. Its
# authorized key (the public half of the GitHub production SSH secret) comes
# from metadata and is written, not appended, so a re-run converges instead of
# accumulating duplicate lines.
id deploy >/dev/null 2>&1 || useradd --create-home --shell /bin/bash deploy
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
metadata eve-trader-deploy-key | write_if_changed /home/deploy/.ssh/authorized_keys 600 || true
chown deploy:deploy /home/deploy/.ssh/authorized_keys

# Staging directory the deploy workflow scps the operator scripts into; owned
# by deploy so no sudo is needed to upload.
install -d -m 755 -o deploy -g deploy /home/deploy/staging

# Fixed, root-owned helper the deploy workflow runs via sudo to install the
# operator scripts from staging. Keeping installation behind one fixed command
# lets the deploy user own the scripts (they version with the app) without
# granting broad root: sudoers below only permits this helper and the runtime
# scripts, never an arbitrary `install`/`tee`. Note this constrains the
# mechanism, not the content -- deploy owns the staging dir, so this is only as
# trusted as the CI identity that already holds scoped sudo on these scripts.
cat >/usr/local/sbin/eve-trader-install-scripts <<'INSTALLER'
#!/bin/sh
# Installs the eve-trader operator scripts from the deploy user's staging
# directory as root-owned files. Run as root (via sudo) by the deploy and
# secrets workflows after they scp the scripts into place. Idempotent.
set -eu
staging=/home/deploy/staging
for s in eve-trader-start eve-trader-deploy eve-trader-secrets eve-trader-backup; do
    [ -f "$staging/$s" ] && install -m 0755 -o root -g root "$staging/$s" "/usr/local/sbin/$s"
done
[ -f "$staging/eve-trader-backup.cron" ] &&
    install -m 0644 -o root -g root "$staging/eve-trader-backup.cron" /etc/cron.d/eve-trader-backup
:
INSTALLER
chmod 0755 /usr/local/sbin/eve-trader-install-scripts

# Validate the sudoers drop-in on a temp file before installing it, so a bad
# edit can never leave an unparseable file in /etc/sudoers.d (which would break
# sudo for the deploy user).
sudoers=$(mktemp)
cat >"$sudoers" <<'SUDOERS'
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-start
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-deploy
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-secrets
deploy ALL=(root) NOPASSWD: /usr/local/sbin/eve-trader-install-scripts
deploy ALL=(root) NOPASSWD: /usr/bin/cat /var/lib/eve-trader/previous-image
SUDOERS
visudo -cf "$sudoers"
install -m 440 "$sudoers" /etc/sudoers.d/eve-trader-deploy
rm -f "$sudoers"

# --- Caddy configuration --------------------------------------------------
# Caddyfile and the systemd drop-in come verbatim from metadata; the domain
# and non-secret app env are rendered from metadata values. The packaged Caddy
# unit does not read /etc/default/caddy, so the drop-in loads it and exposes
# EVE_TRADER_DOMAIN to the Caddyfile's {$EVE_TRADER_DOMAIN} placeholder.
domain=$(metadata eve-trader-domain)
client_id=$(metadata eve-trader-client-id)
caddy_changed=0

install -d /etc/caddy
metadata eve-trader-caddyfile | write_if_changed /etc/caddy/Caddyfile 0644 && caddy_changed=1

install -d /etc/systemd/system/caddy.service.d
metadata eve-trader-caddy-dropin |
    write_if_changed /etc/systemd/system/caddy.service.d/override.conf 0644 && caddy_changed=1

printf 'EVE_TRADER_DOMAIN=%s\n' "$domain" |
    write_if_changed /etc/default/caddy 0644 && caddy_changed=1

# Non-secret runtime env consumed by the app container via eve-trader-start.
printf 'EVE_TRADER_CALLBACK_URL=https://%s/auth/callback\nEVE_TRADER_ESI_CLIENT_ID=%s\n' \
    "$domain" "$client_id" | write_if_changed /var/lib/eve-trader/env 0644 || true

if [ "$caddy_changed" = 1 ]; then
    systemctl daemon-reload
    systemctl restart caddy
fi

# --- First-boot app secrets ----------------------------------------------
# Generated on the VM and guarded by existence so they never enter metadata or
# Terraform state, and a re-boot never rotates them. The values are arbitrary
# strings (the app SHA-256-hashes them), so 32 random bytes hex-encoded suffice.
# Provision production secrets can later overwrite this file to rotate.
secrets=/var/lib/eve-trader/secrets
if [ ! -f "$secrets" ]; then
    umask 077
    cookie=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
    token=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
    printf 'EVE_TRADER_COOKIE_SECRET=%s\nEVE_TRADER_TOKEN_KEY=%s\n' \
        "$cookie" "$token" >"$secrets"
    chmod 600 "$secrets"
fi

# --- Google Cloud Ops Agent ----------------------------------------------
# Ships ONLY the eve-trader container's json-file stdout to Cloud Logging,
# with the app's structured JSON fields preserved (docs/spec/v1.md §8,
# docs/adr/0003). It tails the existing Docker logs and never touches the
# container's logging driver, so `docker logs eve-trader` keeps working.
#
# Scope is deliberate: eve-trader is the only Docker container (Caddy runs as
# a native systemd service, so its logs are in the journal, not json-file),
# and the custom config below overrides the agent's built-in default_pipeline
# so the host syslog receiver is NOT used -- system/SSH logs are not shipped.

# Install the agent only when its package is absent. The installer is
# idempotent, but gating on package presence keeps the steady-state boot
# offline (the swap step above is offline too).
if ! dpkg-query -W -f='${Status}' google-cloud-ops-agent 2>/dev/null | grep -q 'install ok installed'; then
    curl -fsSL https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh -o /tmp/add-ops-agent-repo.sh
    bash /tmp/add-ops-agent-repo.sh --also-install
    rm -f /tmp/add-ops-agent-repo.sh
fi

# Write the app-scoped logging config, restarting the agent only when the
# config actually changes so a re-run on an unchanged host is a no-op.
config=/etc/google-cloud-ops-agent/config.yaml
mkdir -p "$(dirname "$config")"
tmp=$(mktemp)
cat >"$tmp" <<'YAML'
logging:
  receivers:
    # eve-trader is the only Docker container; Caddy is a native systemd
    # service, so this glob picks up the app container's logs only.
    eve_trader:
      type: files
      include_paths:
        - /var/lib/docker/containers/*/*-json.log
  processors:
    # Docker's json-file driver wraps each stdout line as
    # {"log":"<app line>\n","stream":"stdout","time":"..."}.
    parse_docker:
      type: parse_json
      field: message
    # The app's slog JSONHandler line is in the "log" field; parse it so its
    # fields (time, level, msg, ...) become filterable jsonPayload fields
    # rather than one flattened string.
    parse_app:
      type: parse_json
      field: log
    # slog emits level as DEBUG/INFO/WARN/ERROR; map to Cloud Logging severity.
    set_severity:
      type: modify_fields
      fields:
        severity:
          copy_from: jsonPayload.level
          map_values:
            DEBUG: DEBUG
            INFO: INFO
            WARN: WARNING
            ERROR: ERROR
  service:
    pipelines:
      # Defining default_pipeline overrides the agent's built-in one, so the
      # host syslog receiver is not used: only the container logs ship.
      default_pipeline:
        receivers: [eve_trader]
        processors: [parse_docker, parse_app, set_severity]
YAML
if ! cmp -s "$tmp" "$config"; then
    mv "$tmp" "$config"
    systemctl restart google-cloud-ops-agent
else
    rm -f "$tmp"
fi
