provider "google" {
    version = "3.5.0"

	credentials = file("/home/atsaplin/.config/gcloud/application_default_credentials.json")

    project = "fs-internship"
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
	ip_address_ranges = "10.132.0.0/20"
	main_vm_host = cidrhost(local.ip_address_ranges, 10)
	master_server_host = cidrhost(local.ip_address_ranges, 11)
	fs_server_host = cidrhost(local.ip_address_ranges, 12)
	fs_client_host = cidrhost(local.ip_address_ranges, 13)
}

data "template_file" "master_server_install" {
	template = file("master_server_start.sh")
	vars = {
		storage_bucket_url = google_storage_bucket.common-files-store.url	
		admin_host = local.fs_server_host
		master_server_host = local.master_server_host
	}
}

data "template_file" "fs_server_install" {
	template = file("fs_server_start.sh")
	vars = {
		mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
		storage_bucket_url = google_storage_bucket.common-files-store.url	
		master_server_host = local.master_server_host
		self_host = local.fs_server_host
	}
}

data "template_file" "fs_client_install" {
	template = file("fs_client_start.sh")
	vars = {
		storage_bucket_url = google_storage_bucket.common-files-store.url	
	}
}

data "template_file" "main_vm_install" {
	template = file("main_vm_start.sh")
	vars = {
		mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
		storage_bucket_url = google_storage_bucket.common-files-store.url	
		admin_host = local.fs_server_host
		master_server_host = local.master_server_host
	}
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

	metadata_startup_script = data.template_file.main_vm_install.rendered
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

	metadata_startup_script = data.template_file.master_server_install.rendered
	service_account {
		scopes = ["cloud-platform"]
	}
}

resource "google_compute_instance" "fs_server_instance" {
    name = "fs-server-instance"
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
		network_ip = local.fs_server_host
        access_config {
        }
    }

	metadata_startup_script = data.template_file.fs_server_install.rendered
	service_account {
		scopes = ["cloud-platform"]
	}
}

resource "google_compute_instance" "fs_client_instance" {
    name = "fs-client-instance"
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
		network_ip = local.fs_client_host
        access_config {
        }
    }

	metadata_startup_script = data.template_file.fs_client_install.rendered
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
