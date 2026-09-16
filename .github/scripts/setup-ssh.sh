#!/usr/bin/env bash
# Writes the production SSH deploy key and known_hosts for this job so later
# steps can ssh to the production VM. Shared by the deploy and secrets
# workflows, which must checkout the repository first and set SSH_KEY and
# VM_HOST on the step environment. The key never leaves the runner.
set -euo pipefail
install -d -m 700 ~/.ssh
printf '%s\n' "$SSH_KEY" >~/.ssh/deploy_key
chmod 600 ~/.ssh/deploy_key
ssh-keyscan -H "$VM_HOST" >~/.ssh/known_hosts
