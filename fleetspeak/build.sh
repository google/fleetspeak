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

cat >&2 <<EOF

  **************************************************************
  *                                                            *
  *  Dear Fleetspeak users,                                    *
  *                                                            *
  *  Fleetspeak has become a regular Go module,                *
  *  and you can stop using the build.sh script.               *
  *                                                            *
  *  To build from an existing repo clone, use:                *
  *  $ go install ./cmd/...                                    *
  *                                                            *
  *  To build without first doing a clone, use:                *
  *  $ go install github.com/google/fleetspeak/cmd/...@latest  *
  *  (and replace @latest with the revision of your choice)    *
  *                                                            *
  *  The build.sh script will disappear in the near future.    *
  *                                                            *
  **************************************************************

EOF

readonly ARGV0=${0}

# Make sure Fleetspeak client and server binaries are static.
export CGO_ENABLED=0

# Exit on error.
set -e

# Get absolute path to symlink's directory
SCRIPT_DIR="$(/usr/bin/dirname "$(readlink -e "${ARGV0}" 2>/dev/null || true)")"
if [[ -z "${SCRIPT_DIR}" ]]; then
  SCRIPT_DIR="$(/usr/bin/dirname "${BASH_SOURCE[0]}")"
  if [[ -z "${SCRIPT_DIR}" ]]; then
    /bin/echo 'Failed to resolve script directory.'
    exit 1
  fi
fi
# Go one above this script's directory.
cd "${SCRIPT_DIR}"/..

readonly GOS=$(/usr/bin/find ./cmd -name '*.go')
# ${GOS} should not be double-quoted; disable lint check
# shellcheck disable=SC2086
readonly MAIN_FILES=$(grep -l 'func main' ${GOS})

if ! python -c 'import fleetspeak'; then
  /bin/echo >&2 '
Warning: Python fleetspeak module is not importable.
         You can install it with pip if you intend to use it.
'
  if [[ "${STRICT}" == 'true' ]]; then
    exit 2
  fi
fi

for f in ${MAIN_FILES}; do
  COMPILED="${f%.go}$(go env GOEXE)"
  /bin/echo >&2 "Building ${f} => ${COMPILED}"
  go build -o "${COMPILED}" "${f}"
done

