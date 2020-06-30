#!/usr/bin/env bash
cd $(dirname $0)/../../../
python3 -m venv /tmp/.venv/FLEETSPEAK
/tmp/.venv/FLEETSPEAK/bin/pip install absl-py protobuf fleetspeak > /dev/null
source /tmp/.venv/FLEETSPEAK/bin/activate
go run fleetspeak/src/e2etesting/run_end_to_end_tests.go --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! --num_clients=10 --num_servers=2
