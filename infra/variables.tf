variable "project_id" {
  description = "GCP project in which to create the trader VM."
  type        = string
}

variable "region" {
  description = "GCP region. Must be one of the Always Free eligible regions."
  type        = string
  default     = "us-central1"

  validation {
    condition     = contains(["us-west1", "us-central1", "us-east1"], var.region)
    error_message = "region must be us-west1, us-central1, or us-east1."
  }
}

variable "zone" {
  description = "Zone within the selected region."
  type        = string
  default     = "us-central1-a"

  validation {
    condition     = var.zone == "${var.region}-a" || var.zone == "${var.region}-b" || var.zone == "${var.region}-c" || var.zone == "${var.region}-d" || var.zone == "${var.region}-f"
    error_message = "zone must be a zone in the selected region."
  }
}

variable "name" {
  description = "Name prefix for the VM and related resources."
  type        = string
  default     = "eve-trader"
}

variable "machine_type" {
  description = "VM machine type. e2-micro is Always Free eligible in the selected regions."
  type        = string
  default     = "e2-micro"
}
