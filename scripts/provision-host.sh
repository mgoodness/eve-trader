#!/usr/bin/env bash
#
# Brings a bare e2-micro VM running Container-Optimized OS (COS) to a working
# eve-trader host, using the checked-in config in deploy/. Idempotent -- safe
# to re-run after editing deploy/Caddyfile or this script's docker run
# invocations. See docs/adr/0009-checked-in-host-config.md.
#
# Targets COS specifically, not a generic Linux install: COS ships Docker
# baked into the image (there's no package manager and the root filesystem
# is read-only, so a "docker install" step doesn't apply here), which is
# also why this uses plain `docker run` rather than docker-compose --
# compose isn't installable on COS (its /home is mounted noexec, so even a
# static binary dropped there can't be executed; confirmed live against the
# real host while verifying this script).
#
# This does NOT collect or write any secrets -- it assumes ~/eve-trader.env
# and ~/watchtower.env already exist on the host with real values (copy
# deploy/eve-trader.env.example and deploy/watchtower.env.example there by
# hand first; see those files for what's needed).
#
# Usage:
#   GCP_PROJECT=... GCP_ZONE=... GCP_INSTANCE=eve-trader-vm ./scripts/provision-host.sh

set -euo pipefail

GCP_PROJECT="${GCP_PROJECT:?set GCP_PROJECT}"
GCP_ZONE="${GCP_ZONE:?set GCP_ZONE}"
GCP_INSTANCE="${GCP_INSTANCE:-eve-trader-vm}"
REMOTE_DIR="${REMOTE_DIR:-eve-trader-deploy}"

gcp_ssh() {
    gcloud compute ssh "$GCP_INSTANCE" --project "$GCP_PROJECT" --zone "$GCP_ZONE" --command "$1"
}

echo "==> Checking connectivity to $GCP_INSTANCE..."
gcp_ssh "echo ok" >/dev/null

echo "==> Checking for docker (COS ships it baked in -- this script doesn't install it)..."
if ! gcp_ssh "command -v docker >/dev/null 2>&1"; then
    echo "!! docker isn't present on $GCP_INSTANCE. This script targets Container-Optimized OS," >&2
    echo "!! which ships Docker as part of the base image -- if this host isn't running COS," >&2
    echo "!! install Docker by hand first." >&2
    exit 1
fi

echo "==> Checking for ghcr.io pull access on the host..."
if ! gcp_ssh "docker pull ghcr.io/mgoodness/eve-trader:stable >/dev/null 2>&1"; then
    echo "!! couldn't pull ghcr.io/mgoodness/eve-trader:stable. eve-trader is a private repo," >&2
    echo "!! so a bare VM needs its own ghcr.io credential first: docker login ghcr.io" >&2
    echo "!! with a PAT scoped to read:packages (see scripts/setup-cicd.sh stage 4), then" >&2
    echo "!! re-run this script." >&2
    exit 1
fi

echo "==> Checking for ~/eve-trader.env and ~/watchtower.env on the host..."
if ! gcp_ssh "test -f ~/eve-trader.env"; then
    echo "!! ~/eve-trader.env is missing. Copy deploy/eve-trader.env.example there," >&2
    echo "!! fill in real values, chmod 600 it, then re-run this script." >&2
    exit 1
fi
if ! gcp_ssh "test -f ~/watchtower.env"; then
    echo "!! ~/watchtower.env is missing. Copy deploy/watchtower.env.example there," >&2
    echo "!! fill in a real value, chmod 600 it, then re-run this script." >&2
    exit 1
fi

echo "==> Ensuring eve-trader-net exists..."
gcp_ssh "docker network inspect eve-trader-net >/dev/null 2>&1 || docker network create eve-trader-net"

echo "==> Copying deploy/Caddyfile to ~/$REMOTE_DIR/..."
gcp_ssh "mkdir -p ~/$REMOTE_DIR/data/caddy-data ~/$REMOTE_DIR/data/caddy-config ~/eve-trader-data"
gcloud compute scp --project "$GCP_PROJECT" --zone "$GCP_ZONE" \
    deploy/Caddyfile "$GCP_INSTANCE:~/$REMOTE_DIR/Caddyfile"

echo "==> (Re)starting eve-trader..."
gcp_ssh "docker rm -f eve-trader >/dev/null 2>&1 || true"
gcp_ssh "docker run -d --name eve-trader --restart=always --network eve-trader-net \
  --env-file ~/eve-trader.env -e EVE_TRADER_DB_PATH=/data/eve-trader.db \
  -v ~/eve-trader-data:/data ghcr.io/mgoodness/eve-trader:stable"

echo "==> (Re)starting watchtower..."
gcp_ssh "docker rm -f watchtower >/dev/null 2>&1 || true"
# Single-quoted deliberately: \$HOME must expand on the VM, not here.
# shellcheck disable=SC2016
gcp_ssh 'docker run -d --name watchtower --restart=always --network eve-trader-net \
  -p 127.0.0.1:8080:8080 --env-file ~/watchtower.env -e WATCHTOWER_HTTP_API_UPDATE=true \
  -v /var/run/docker.sock:/var/run/docker.sock -v $HOME/.docker/config.json:/config.json:ro \
  containrrr/watchtower --interval 300 --http-api-metrics eve-trader'

echo "==> (Re)starting caddy..."
gcp_ssh "docker rm -f caddy >/dev/null 2>&1 || true"
gcp_ssh "docker run -d --name caddy --restart=always --network eve-trader-net \
  -p 80:80 -p 443:443 \
  -v ~/$REMOTE_DIR/Caddyfile:/etc/caddy/Caddyfile \
  -v ~/$REMOTE_DIR/data/caddy-data:/data \
  -v ~/$REMOTE_DIR/data/caddy-config:/config \
  caddy:2"

echo "==> Done. Current status:"
gcp_ssh "docker ps --filter name=eve-trader --filter name=watchtower --filter name=caddy --format '{{.Names}}: {{.Status}}'"
