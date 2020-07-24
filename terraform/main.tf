variable "project_name" {
    type = string
}

variable "num_clients" {
    type = number
    default = 1
}

variable "num_servers" {
    type = number
    default = 1
}


provider "google" {
    version = "3.5.0"

    project = var.project_name
    region = "europe-west1"
    zone = "europe-west1-b"
}

resource "google_compute_network" "vpc_network" {
    name = "terraform-network"
}

resource "google_compute_firewall" "allow-ssh" {
    name = "allow-ssh"
    network = google_compute_network.vpc_network.self_link

    allow {
        protocol = "tcp"
        ports = ["22"]
    }

    source_ranges = ["0.0.0.0/0"]
    target_tags = ["ssh"]
}

locals {
    ip_address_ranges_prefix = "10.132.0"
    ip_address_ranges = format("%s.0/20", local.ip_address_ranges_prefix)
    main_vm_host = cidrhost(local.ip_address_ranges, 10)
    master_server_host = cidrhost(local.ip_address_ranges, 11)
    first_fs_server_host_suffix = 12
    fs_admin_host = cidrhost(local.ip_address_ranges, local.first_fs_server_host_suffix)
    first_fs_client_host_suffix = local.first_fs_server_host_suffix + var.num_servers
}

resource "random_id" "bucket_name_suffix" {
    byte_length = 4
}

resource "google_storage_bucket" "common-files-store" {
    name = "common-files-store-${random_id.bucket_name_suffix.hex}"
    location = "EU"
    force_destroy = true
    bucket_policy_only = true
}

resource "google_compute_instance" "vm_instance" {
    name = "terraform-instance"
    machine_type = "n1-standard-1"

    tags = ["ssh"]

    boot_disk {
        initialize_params {
            image = "projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
        }
    }

    depends_on = [
        google_compute_network.vpc_network,
    ]

    network_interface {
        network = google_compute_network.vpc_network.self_link
        network_ip = local.main_vm_host
        access_config {
        }
    }

    metadata_startup_script = templatefile(
        "main_vm_start.sh",
        {
            mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
            storage_bucket_url = google_storage_bucket.common-files-store.url
            admin_host = local.fs_admin_host
            master_server_host = local.master_server_host
            ip_address_ranges_prefix = local.ip_address_ranges_prefix
            first_fs_server_host_suffix = local.first_fs_server_host_suffix
            num_clients = var.num_clients
            num_servers = var.num_servers
        }
    )

    service_account {
        scopes = ["cloud-platform"]
    }
}

resource "google_compute_instance" "master_server_instance" {
    name = "master-server-instance"
    machine_type = "n1-standard-1"

    tags = ["ssh"]

    boot_disk {
        initialize_params {
            image = "projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
        }
    }

    depends_on = [
        google_compute_network.vpc_network,
    ]

    network_interface {
        network = google_compute_network.vpc_network.self_link
        network_ip = local.master_server_host
        access_config {
        }
    }

    metadata_startup_script = templatefile(
        "master_server_start.sh",
        {
            storage_bucket_url = google_storage_bucket.common-files-store.url
            admin_host = local.fs_admin_host
            master_server_host = local.master_server_host
        }
    )

    service_account {
        scopes = ["cloud-platform"]
    }
}

resource "google_compute_instance" "fs_server_instance" {
    count = var.num_servers
    name = "fs-server-instance-${count.index}"
    machine_type = "n1-standard-1"

    tags = ["ssh"]

    boot_disk {
        initialize_params {
            image = "projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
        }
    }

    depends_on = [
        google_compute_network.vpc_network,
    ]

    network_interface {
        network = google_compute_network.vpc_network.self_link
        network_ip = cidrhost(local.ip_address_ranges, local.first_fs_server_host_suffix + count.index)
        access_config {
        }
    }

    metadata_startup_script = templatefile(
        "fs_server_start.sh",
        {
            mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
            storage_bucket_url = google_storage_bucket.common-files-store.url
            master_server_host = local.master_server_host
            self_host = cidrhost(local.ip_address_ranges, local.first_fs_server_host_suffix + count.index)
            self_index = count.index
        }
    )

    service_account {
        scopes = ["cloud-platform"]
    }
}

resource "google_compute_instance" "fs_client_instance" {
    count = var.num_clients
    name = "fs-client-instance-${count.index}"
    machine_type = "n1-standard-1"

    tags = ["ssh"]

    boot_disk {
        initialize_params {
            image = "projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
        }
    }

    depends_on = [
        google_compute_network.vpc_network,
    ]

    network_interface {
        network = google_compute_network.vpc_network.self_link
        network_ip = cidrhost(local.ip_address_ranges, local.first_fs_client_host_suffix + count.index)
        access_config {
        }
    }

    metadata_startup_script = templatefile(
        "fs_client_start.sh",
        {
            storage_bucket_url = google_storage_bucket.common-files-store.url
            self_index = count.index
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
    name = "fs-db-instance-1-${random_id.db_name_suffix.hex}"
    region = "europe-west1"

    settings {
        tier = "db-n1-standard-1"

        database_flags {
            name = "max_allowed_packet"
            value = "1073741824"
        }

        database_flags {
            name = "log_output"
            value = "FILE"
        }

        database_flags {
            name = "slow_query_log"
            value = "on"
        }
    }
}

resource "google_sql_user" "users" {
    name = "fsuser"
    instance = google_sql_database_instance.fs-db.name
    password = "fsuserPass1!"
}

resource "google_sql_database" "fs-db" {
    name = "fleetspeak_test"
    instance = google_sql_database_instance.fs-db.name
    charset = "utf8mb4"
    collation = "utf8mb4_unicode_ci"
}

output "mysql_host" {
    value = google_sql_database_instance.fs-db.ip_address[0].ip_address
}
