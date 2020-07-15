provider "google" {
    version = "3.5.0"

	credentials = file("/home/atsaplin/.config/gcloud/application_default_credentials.json")

    project = "fs-internship"
    region  = "us-central1"
    zone    = "us-central1-c"
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

data "template_file" "fs_server_install" {
	template = file("server_start.sh")
	vars = {
		mysql_instance_connection_name = google_sql_database_instance.fs-db.connection_name
	}
}

resource "google_compute_instance" "vm_instance" {
    name         = "terraform-instance"
    machine_type = "n1-standard-1"

	tags = ["ssh"]

    boot_disk {
        initialize_params {
            image = "projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
        }
    }

    network_interface {
        network = google_compute_network.vpc_network.self_link
        access_config {
        }
    }

	metadata_startup_script = data.template_file.fs_server_install.rendered
	service_account {
		scopes = ["cloud-platform"]
	}
}

resource "random_id" "db_name_suffix" {
	byte_length = 4
}

resource "google_sql_database_instance" "fs-db" {
    name   = "fs-db-instance-1-${random_id.db_name_suffix.hex}"
    region  = "us-central1"

    settings {
        tier = "db-n1-standard-1"

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

output "mysql_host" {
	value = google_sql_database_instance.fs-db.ip_address[0].ip_address
}
