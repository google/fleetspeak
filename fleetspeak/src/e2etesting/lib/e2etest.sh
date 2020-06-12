#!/usr/bin/env bash
python3 -m venv /tmp/.venv/FLEETSPEAK
/tmp/.venv/FLEETSPEAK/bin/pip install absl-py protobuf fleetspeak > /dev/null
source /tmp/.venv/FLEETSPEAK/bin/activate
go run setup_components.go --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1!
