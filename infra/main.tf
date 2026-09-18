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

  # Adds the 2 GB swap file and installs the Google Cloud Ops Agent on every
  # boot (docs/spec/v1.md §8, docs/adr/0003). Set via the mutable metadata map
  # (not metadata_startup_script, which is ForceNew) so script edits update in
  # place instead of replacing the VM and its disk.
  metadata = {
    startup-script = file("${path.module}/startup.sh")
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
