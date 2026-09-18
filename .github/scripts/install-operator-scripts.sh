#!/usr/bin/env bash
# Uploads the eve-trader operator scripts to the VM's staging directory and
# installs them as root-owned files via the fixed eve-trader-install-scripts
# helper (created by the Terraform startup script). The operator scripts version
# with the application, so the deploy and secrets workflows own their
# installation rather than a `terraform apply` (docs/deployment.md, infra/README.md).
#
# Shared by the deploy and secrets workflows, which must checkout the repository
# and configure SSH first, and set VM_HOST and VM_USER on the step environment.
# Idempotent.
set -euo pipefail
scp -i ~/.ssh/deploy_key \
    infra/eve-trader-start \
    infra/eve-trader-deploy \
    infra/eve-trader-secrets \
    infra/eve-trader-backup \
    infra/eve-trader-backup.cron \
    "$VM_USER@$VM_HOST:staging/"
ssh -i ~/.ssh/deploy_key "$VM_USER@$VM_HOST" 'sudo /usr/local/sbin/eve-trader-install-scripts'
