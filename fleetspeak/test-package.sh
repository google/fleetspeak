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
/usr/bin/fleetspeak-config --config=/etc/fleetspeak-server/configurator.config

/bin/echo 'Checking that the installation was successful'
ls -l /etc/fleetspeak-server
find /etc/systemd/ -name 'fleetspeak*'
# TODO(mbushkov): uncomment the checks below as soon as Travis build is
# Xenial- (and not Trusty-) based. See .travis.yml for more context re
# why Trusty is still used as a build platform.
#
# # At this point the service is down, since right after the installation it was
# # started without a configuration.
# systemctl restart fleetspeak-server
# # Give the service a bit of time to start.
# sleep 1
# # Check that it's now up and running.
# systemctl is-active fleetspeak-server
