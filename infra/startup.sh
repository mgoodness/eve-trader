#!/bin/sh
# GCE instance startup script, run on every boot. Idempotent: every step is
# safe to re-run and reuses existing state rather than recreating it.
#   1. A 2 GB swap file as an OOM safety net for the 1 GB e2-micro.
#   2. The Google Cloud Ops Agent, which ships the eve-trader container's
#      json-file stdout to Cloud Logging (docs/spec/v1.md §8, docs/adr/0003).
set -eu

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
