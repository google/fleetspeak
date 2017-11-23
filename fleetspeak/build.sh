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


readonly ARGV0=${0}

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

readonly GOS=$(/usr/bin/find . -name '*.go')
# ${GOS} should not be double-quoted; disable lint check
# shellcheck disable=SC2086
readonly MAIN_FILES=$(grep -rl 'package main' ${GOS})

export TIMEFORMAT='real %lR user %lU system %lS'

if ! python -c 'import fleetspeak'; then
  /bin/echo >&2 '
Warning: Python fleetspeak module is not importable.
         You can install it with pip if you intend to use it.
'
  if [[ "${STRICT}" == 'true' ]]; then
    exit 2
  fi
fi

function build_single_main_file {
  local readonly F=${1}
  # `%.go' means strip the the `.go' suffix.
  if [[ "$(uname)" == 'CYGWIN'* ]]; then
    local readonly COMPILED="${F%.go}.exe"
  else
    local readonly COMPILED="${F%.go}"
  fi

  time (
    /bin/echo >&2 "Building ${F} => ${COMPILED} "
    go build -o "${COMPILED}" "${F}"
  )
}

time (
  for f in ${MAIN_FILES}; do
    build_single_main_file "${f}"
  done

  /bin/echo >&2 -n "${ARGV0} "
)
