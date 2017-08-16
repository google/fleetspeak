#!/bin/bash
# Copyright 2017 Google Inc.
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

#
# Unit test for client.
#
# This script reserves ports and then starts two processes which communicate
# with each other using these ports:
#
# 1) A loopback python script based on the grpcservice client library.
#
# 2) A tester which is, primarily, a fleetspeak server with the grpcservice
# installed.
#
# Then the tester sends a message through the loopback, as if it came from a
# client, and waits for the looped message to come back to the same client.
#
# (Alternatively, this could be done by spawning loopback with tester, but using
# independent processes is more realistic.)

# Exit on error.
set -e

readonly LOOPBACK='src/server/grpcservice/client/testing/loopback.py'
readonly TESTER='src/server/grpcservice/client/testing/tester'

function randomize_tcp_port {
  local readonly MINPORT=32760
  local readonly MAXPORT=59759
  /bin/echo $(( MINPORT + RANDOM%(MAXPORT-MINPORT+1) ))
}

readonly MESSAGE_PORT=$(randomize_tcp_port)
readonly ADMIN_PORT=$(randomize_tcp_port)

# Start loopback in the background, kill when finished. We do not care about its
# exit code.
python "${LOOPBACK}" \
  --fleetspeak_message_listen_address="localhost:${MESSAGE_PORT}" \
  --fleetspeak_server="localhost:${ADMIN_PORT}" &
readonly PID=${!}
trap "/bin/kill -- $PID ; wait" EXIT SIGINT

# If anything goes wrong the tester should return a non-zero exit code and as a
# unit test we should preserve this.
"${TESTER}" \
  --admin_addr="localhost:${ADMIN_PORT}" \
  --message_addr="localhost:${MESSAGE_PORT}"
