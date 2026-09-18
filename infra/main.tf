resource "google_compute_address" "external" {
  name   = "${var.name}-ip"
  region = var.region
}

# Dedicated, least-privilege service account for the VM. It exists so the
# Google Cloud Ops Agent can ship the app container's logs to Cloud Logging
# without granting the instance the broad default compute service account
# (docs/adr/0003, docs/spec/v1.md §8).
resource "google_service_account" "vm" {
  account_id   = "${var.name}-vm"
  display_name = "${var.name} VM (Ops Agent log writer)"
}

# The only role the instance needs: write log entries to Cloud Logging.
resource "google_project_iam_member" "vm_log_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.vm.email}"
}

resource "google_compute_firewall" "https" {
  name    = "${var.name}-https"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  direction     = "INGRESS"
  source_ranges = ["0.0.0.0/0"]
  target_tags   = [var.name]
}

resource "google_compute_instance" "app" {
  name         = var.name
  machine_type = var.machine_type
  zone         = var.zone
  tags         = [var.name]

  # Runs the full OS bootstrap on every boot (docs/spec/v1.md §8,
  # docs/adr/0003): OS packages, swap, state dir, deploy user, Caddy config,
  # and first-boot secret generation. Set via the mutable metadata map (not
  # metadata_startup_script, which is ForceNew) so script edits update in place
  # instead of replacing the VM and its disk.
  #
  # The non-secret config values and the verbatim Caddy config files are passed
  # as additional metadata attributes, which the startup script reads from the
  # metadata server. These land in Terraform state, which is fine: none are
  # secret. The app secrets are generated on the VM and never come through here.
  metadata = {
    startup-script          = file("${path.module}/startup.sh")
    eve-trader-domain       = var.domain
    eve-trader-client-id    = var.esi_client_id
    eve-trader-deploy-key   = var.deploy_public_key
    eve-trader-caddyfile    = file("${path.module}/Caddyfile")
    eve-trader-caddy-dropin = file("${path.module}/caddy-environment.conf")
  }

  # Bind the dedicated service account with only the logging.write scope, so
  # the Ops Agent can write logs and nothing else. IAM (logging.logWriter
  # above) is the real authority; the scope is the coarse OAuth ceiling.
  service_account {
    email  = google_service_account.vm.email
    scopes = ["https://www.googleapis.com/auth/logging.write"]
  }

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 30
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"

    access_config {
      nat_ip = google_compute_address.external.address
    }
  }

  scheduling {
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
  }
}
