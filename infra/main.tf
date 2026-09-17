resource "google_compute_address" "external" {
  name   = "${var.name}-ip"
  region = var.region
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

  # Adds the 2 GB swap file on every boot (docs/spec/v1.md §8).
  metadata_startup_script = file("${path.module}/startup.sh")

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
