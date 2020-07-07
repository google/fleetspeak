#!/usr/bin/env bash
cd $(dirname $0)/../../../
python3 -m venv /tmp/.venv/FLEETSPEAK
/tmp/.venv/FLEETSPEAK/bin/pip install fleetspeak > /dev/null
source /tmp/.venv/FLEETSPEAK/bin/activate
pip install -e frr_python
go run fleetspeak/src/e2etesting/run_end_to_end_tests.go --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1!
