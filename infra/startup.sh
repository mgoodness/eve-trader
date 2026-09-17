#!/bin/sh
# GCE instance startup script, run on every boot. Adds a 2 GB swap file as an
# OOM safety net for the 1 GB e2-micro (docs/spec/v1.md §8). Idempotent: it
# reuses an existing swap file rather than recreating it.
set -eu
file=/swapfile

if swapon --show=NAME --noheadings | grep -qx "$file"; then
    exit 0
fi

if [ ! -f "$file" ]; then
    fallocate -l 2G "$file" || dd if=/dev/zero of="$file" bs=1M count=2048
    chmod 600 "$file"
    mkswap "$file"
fi

swapon "$file"
grep -qE "^${file}[[:space:]]" /etc/fstab ||
    printf '%s none swap sw 0 0\n' "$file" >>/etc/fstab
