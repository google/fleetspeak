variable "project_name" {
  type = string
}

variable "num_clients" {
  type    = number
  default = 1
}

variable "num_servers" {
  type    = number
  default = 1
}

variable "vm_image" {
  type    = string
  default = "ubuntu-1804-bionic-v20200317"
}

locals {
  ip_address_ranges   = "10.132.0.0/20"
  ip_fs_server_prefix = "10.132.1"
  ip_fs_client_prefix = "10.132.2"
  fs_admin_host       = format("%s.0", local.ip_fs_server_prefix)
  main_vm_host        = cidrhost(local.ip_address_ranges, 10)
  master_server_host  = cidrhost(local.ip_address_ranges, 11)
  lb_frontend_ip      = cidrhost(local.ip_address_ranges, 12)
}

provider "google" {
  version = "3.5.0"

  project = var.project_name
  region  = "europe-west1"
  zone    = "europe-west1-b"
}

resource "google_compute_network" "vpc_network" {
  name                    = "terraform-network"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "vpc_subnetwork" {
  name          = "terraform-network"
  ip_cidr_range = "10.132.0.0/20"
  region        = "europe-west1"
  network       = google_compute_network.vpc_network.id
}

resource "google_compute_firewall" "allow-tcp" {
  name    = "allow-tcp"
  network = google_compute_network.vpc_network.self_link

  allow {
    protocol = "tcp"
  }

  source_ranges = [local.ip_address_ranges]
  target_tags   = ["allow-tcp"]
}

/*
  In external projects the health checks work properly.
  In internal GCP projects non-default firewall rules may be automatically removed at some moment,
  but the traffic is distributed to all backend VMs, even if all the VMs are considered unhealthy
  (https://cloud.google.com/load-balancing/docs/health-check-concepts#importance_of_firewall_rules).
*/

resource "google_compute_firewall" "allow-health-checks" {
  name    = "allow-health-checks"
  network = google_compute_network.vpc_network.self_link

  allow {
    protocol = "all"
  }

  source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
  target_tags   = ["allow-health-checks"]
}

// TODO(Alexandr-TS): Add firewall rules denying all the IPs except the Load balancer (local.lb_frontend_ip) to connect to fleetspeak servers.

resource "google_compute_health_check" "http-health-check" {
  name = "http-health-check"

  timeout_sec         = 5
  check_interval_sec  = 10
  healthy_threshold   = 1
  unhealthy_threshold = 3

  http_health_check {
    port = "8085"
  }
}

resource "google_compute_forwarding_rule" "load-balancer-forwarding-rule" {
  name                  = "load-balancer-forwarding-rule"
  ip_address            = local.lb_frontend_ip
  load_balancing_scheme = "INTERNAL"
  backend_service       = google_compute_region_backend_service.fs-backend.id
  ports                 = ["6060"]
  network               = google_compute_network.vpc_network.name
  subnetwork            = google_compute_subnetwork.vpc_subnetwork.name
}

resource "google_compute_region_backend_service" "fs-backend" {
  name   = "fs-backend"
  region = "europe-west1"
  backend {
    group = google_compute_instance_group.fs-servers.id
  }
  health_checks = [google_compute_health_check.http-health-check.id]
}

resource "random_id" "bucket_name_suffix" {
  byte_length = 4
}

resource "google_storage_bucket" "common-files-store" {
  name               = "common-files-store-${random_id.bucket_name_suffix.hex}"
  location           = "EU"
  force_destroy      = true
  bucket_policy_only = true
}

resource "google_compute_instance" "vm_instance" {
  name         = "terraform-instance"
  machine_type = "n1-standard-1"

  tags = ["allow-tcp"]

  boot_disk {
    initialize_params {
      image = var.vm_image
    }
  }

  depends_on = [
    google_compute_network.vpc_network,
  ]

  network_interface {
    network    = google_compute_network.vpc_network.self_link
    subnetwork = google_compute_subnetwork.vpc_subnetwork.self_link
    network_ip = local.main_vm_host
    access_config {
    }
  }

  metadata_startup_script = templatefile(
    "main_vm_start.sh",
    {
      mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
      storage_bucket_url             = google_storage_bucket.common-files-store.url
      admin_host                     = local.fs_admin_host
      master_server_host             = local.master_server_host
      ip_fs_server_prefix            = local.ip_fs_server_prefix
      num_servers                    = var.num_servers
      num_clients                    = var.num_clients
      lb_frontend_ip                 = local.lb_frontend_ip
    }
  )

  service_account {
    scopes = ["cloud-platform"]
  }
}

resource "google_compute_instance" "master_server_instance" {
  name         = "master-server-instance"
  machine_type = "n1-standard-1"

  tags = ["allow-tcp"]

  boot_disk {
    initialize_params {
      image = var.vm_image
    }
  }

  depends_on = [
    google_compute_network.vpc_network,
  ]

  network_interface {
    network    = google_compute_network.vpc_network.self_link
    subnetwork = google_compute_subnetwork.vpc_subnetwork.self_link
    network_ip = local.master_server_host
    access_config {
    }
  }

  metadata_startup_script = templatefile(
    "master_server_start.sh",
    {
      storage_bucket_url = google_storage_bucket.common-files-store.url
      admin_host         = local.fs_admin_host
      master_server_host = local.master_server_host
    }
  )

  service_account {
    scopes = ["cloud-platform"]
  }
}

resource "google_compute_instance_group" "fs-servers" {
  name      = "fs-servers"
  instances = [for instance in google_compute_instance.fs_server_instance : instance.self_link]
  zone      = "europe-west1-b"
}

resource "google_compute_instance" "fs_server_instance" {
  count        = var.num_servers
  name         = "fs-server-instance-${count.index}"
  machine_type = "n1-standard-1"

  tags = ["allow-tcp", "allow-health-checks"]

  boot_disk {
    initialize_params {
      image = var.vm_image
    }
  }

  depends_on = [
    google_compute_network.vpc_network,
  ]

  network_interface {
    network    = google_compute_network.vpc_network.self_link
    subnetwork = google_compute_subnetwork.vpc_subnetwork.self_link
    network_ip = format("%s.%s", local.ip_fs_server_prefix, count.index)
    access_config {
    }
  }

  metadata_startup_script = templatefile(
    "fs_server_start.sh",
    {
      mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
      storage_bucket_url             = google_storage_bucket.common-files-store.url
      master_server_host             = local.master_server_host
      self_host                      = format("%s.%s", local.ip_fs_server_prefix, count.index)
      self_index                     = count.index
    }
  )

  service_account {
    scopes = ["cloud-platform"]
  }
}

resource "google_compute_instance" "fs_client_instance" {
  count        = var.num_clients
  name         = "fs-client-instance-${count.index}"
  machine_type = "n1-standard-1"

  tags = ["allow-tcp"]

  boot_disk {
    initialize_params {
      image = var.vm_image
    }
  }

  depends_on = [
    google_compute_network.vpc_network,
  ]

  network_interface {
    network    = google_compute_network.vpc_network.self_link
    subnetwork = google_compute_subnetwork.vpc_subnetwork.self_link
    network_ip = format("%s.%s", local.ip_fs_client_prefix, count.index)
    access_config {
    }
  }

  metadata_startup_script = templatefile(
    "fs_client_start.sh",
    {
      storage_bucket_url = google_storage_bucket.common-files-store.url
      self_index         = count.index
    }
  )

  service_account {
    scopes = ["cloud-platform"]
  }
}

resource "random_id" "db_name_suffix" {
  byte_length = 4
}

resource "google_sql_database_instance" "fs-db" {
  name   = "fs-db-instance-1-${random_id.db_name_suffix.hex}"
  region = "europe-west1"

  settings {
    tier = "db-f1-micro"

    database_flags {
      name  = "max_allowed_packet"
      value = "1073741824"
    }

    database_flags {
      name  = "log_output"
      value = "FILE"
    }

    database_flags {
      name  = "slow_query_log"
      value = "on"
    }
  }
}

resource "google_sql_user" "users" {
  name     = "fsuser"
  instance = google_sql_database_instance.fs-db.name
  password = "fsuserPass1!"
}

resource "google_sql_database" "fs-db" {
  name      = "fleetspeak_test"
  instance  = google_sql_database_instance.fs-db.name
  charset   = "utf8mb4"
  collation = "utf8mb4_unicode_ci"
}
