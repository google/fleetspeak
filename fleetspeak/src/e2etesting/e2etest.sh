#!/usr/bin/env bash
cd $(dirname $0)/../../../
pip3 install -e frr_python
go run fleetspeak/src/e2etesting/run_end_to_end_tests.go --mysql_address=127.0.0.1:3306 --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! --num_clients=1 --num_servers=1
