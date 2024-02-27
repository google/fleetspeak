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


# Get absolute path to symlink's directory
set +e # Do not exit if readlink errors out
SCRIPT_DIR="$(/usr/bin/dirname "$(readlink -e "${ARGV0}" 2>/dev/null)")"
# Exit on error.
set -e
if [[ -z "${SCRIPT_DIR}" || "${SCRIPT_DIR}" == '.' ]]; then
  SCRIPT_DIR="$(/usr/bin/dirname "${BASH_SOURCE[0]}")"
  if [[ -z "${SCRIPT_DIR}" ]]; then
    /bin/echo 'Failed to resolve script directory.'
    exit 1
  fi
fi
# Go to this script's directory.
cd "${SCRIPT_DIR}"

readonly TEST_SHS='src/server/grpcservice/client/testing/client_test.sh'

function test_single_sh {
  local readonly SH=${1}

  OUT=$("${SH}" 2>&1)

  if [ "$?" -ne 0 ]; then
    /bin/echo "FAIL ${SH}"
    /bin/echo "${OUT}"
    return 1
  fi

  /bin/echo "PASS ${SH}"
}

function pretty_echo {
  /bin/echo '---'
  /bin/echo "${@}"
  /bin/echo '---'
}

RC=0

pretty_echo 'Building prerequisites for Go tests.'
go build -o ./src/e2etesting/frr_master_server_main/frr_master_server_main{,.go}
go build -o ./src/e2etesting/balancer/balancer{,.go}
go build -o ./src/client/daemonservice/testclient/testclient{,.go}
go build -o ./src/client/socketservice/testclient/testclient{,.go}
go build -o ./src/server/grpcservice/client/testing/tester{,.go}
cd ..
go build -o ./cmd/fleetspeak_config/fleetspeak_config{,.go}
cd -

pretty_echo 'Executing Go tests.'
go test -race --timeout 2.5m ./... || RC=1

pretty_echo 'Executing Python tests.'
pytest -v ../fleetspeak_python || RC=2

pretty_echo 'Executing Bash tests.'
for s in ${TEST_SHS}; do
  test_single_sh "${s}" || RC=3
done

exit "${RC}"
