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
if [[ -z "${SCRIPT_DIR}" ]]; then
  SCRIPT_DIR="$(/usr/bin/dirname "${BASH_SOURCE[0]}")"
  if [[ -z "${SCRIPT_DIR}" ]]; then
    /bin/echo 'Failed to resolve script directory.'
    exit 1
  fi
fi
# Go to this script's directory.
cd "${SCRIPT_DIR}"

readonly TEST_GOS=$(/usr/bin/find . -name '*_test.go')
readonly TEST_SHS='src/server/grpcservice/client/testing/client_test.sh'
# Note that the MacOS version of dirname cannot take multiple inputs.
# ${TEST_GOS} should not be double-quoted.
# shellcheck disable=SC2086
TEST_GO_DIRS=$(for test_file in ${TEST_GOS}; do /usr/bin/dirname $test_file; done)
# ${TEST_GO_DIRS} should not be double-quoted.
# shellcheck disable=SC2086
readonly TEST_GO_DIRS=$(echo ${TEST_GO_DIRS} | /usr/bin/sort | /usr/bin/uniq)

export TIMEFORMAT='real %lR user %lU system %lS'

function test_single_sh {
  local readonly SH=${1}

  time (
    OUT=$("${SH}" 2>&1) \
        && /bin/echo -n "PASS ${SH} " \
        || /bin/echo -n "FAIL ${SH}
${OUT}
"
  ) 2>&1
}

function pretty_echo {
  /bin/echo '---'
  /bin/echo "${@}"
  /bin/echo '---'
}

time (
  RC=0

  pretty_echo 'Executing Go tests.'
  time go test --timeout 1m ${TEST_GO_DIRS} || RC=1

  pretty_echo 'Executing Python tests.'
  python -m unittest discover --pattern '*_test.py' || RC=2

  pretty_echo 'Executing Bash tests.'
  for s in ${TEST_SHS}; do
    test_single_sh "${s}" || RC=3
  done

  # This will prefix time's output.
  /bin/echo -n "
${ARGV0} "

  exit "${RC}"
) 2>&1
# RC will be forwarded because `set -e' is in effect.
