#!/usr/bin/env bash
cd $(dirname $0)/../../../
python3 -m venv /tmp/.venv/FLEETSPEAK
source /tmp/.venv/FLEETSPEAK/bin/activate
pip3 install -e frr_python
go run fleetspeak/src/e2etesting/run_end_to_end_tests.go --mysql_address=127.0.0.1:3306 --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! --num_clients=8 --num_servers=3
