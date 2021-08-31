#!/bin/bash
# Copyright 2019 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -ex

/bin/echo 'Installing newly built server package.'
apt install -y $1

if [[ ! -z "$MYSQL_TEST_USER" ]]
then
    sed -i "s/mysql_data_source_name: .*/mysql_data_source_name: $MYSQL_TEST_USER:$MYSQL_TEST_PASS@tcp($MYSQL_TEST_ADDR)\/$MYSQL_TEST_E2E_DB/g" /etc/fleetspeak-server/configurator.config
fi

-u fleetspeak /usr/bin/fleetspeak-config --config=/etc/fleetspeak-server/configurator.config

/bin/echo 'Checking that the installation was successful'
ls -l /etc/fleetspeak-server
find /etc/systemd/ -name 'fleetspeak*'

# At this point the service is down, since right after the installation it was
# started without a configuration.
# Reset a list of failed services to ensure the restart below works fine.
systemctl reset-failed

# Restart the service.
systemctl restart fleetspeak-server

# Check that it's now up and running.
systemctl is-active fleetspeak-server

# Now copy the linux client configuration to the expected location.
mkdir -p /etc/fleetspeak-client
cp /etc/fleetspeak-server/linux.client.configuration /etc/fleetspeak-client/client.config

# Install the client package.
apt install -y $2

# Check that the client is up and running.
systemctl is-active fleetspeak-client

sleep 10
journalctl -u fleetspeak-server
journalctl -u fleetspeak-client
systemctl -l status fleetspeak-server
systemctl -l status fleetspeak-client

# Check that fleetspeak_admin functions and returns info about a single client we have.
fleetspeak-admin -admin_addr localhost:9000 listclients 
# | grep "[a-z0-9]\{16\} .*client:linux"
