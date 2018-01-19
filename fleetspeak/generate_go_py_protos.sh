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


# Exit on error.
set -e

readonly ARGV0=${0}

# Go to this script's directory.
cd "$(/usr/bin/dirname "$(/bin/readlink -e "${ARGV0}")")"

# If PROTOC isn't set:
if [[ -z "${PROTOC}" ]]; then
  readonly PROTOC='/usr/local/bin/protoc'
else
  readonly PROTOC=${PROTOC}
fi

# If PROTOC is not an executable:
if ! [[ -x "${PROTOC}" ]]; then
  /bin/echo >&2 "
protoc is not available. Either install protoc, or point this script to protoc using:
  PROTOC=/path/to/protoc ${ARGV0}
"
  exit 1
fi

# If SRC_PATH isn't set:
if [[ -z "${SRC_PATH}" ]]; then
  readonly SRC_PATH="${GOPATH}/src/"
else
  readonly SRC_PATH=${SRC_PATH}
fi

readonly FLEETSPEAK_PATH="${SRC_PATH}/github.com/google/fleetspeak/"

readonly PROTOS=$(/usr/bin/find "${FLEETSPEAK_PATH}/fleetspeak/" -name '*.proto')
readonly PROTO_PACKAGES=$(/usr/bin/dirname ${PROTOS} | /usr/bin/uniq)

function generate_protoc_m_opts {
  for p in ${PROTOS}; do
    local RELATIVE_P=$(/bin/sed "s:^${FLEETSPEAK_PATH}/::" <<< "${p}")
    local RELATIVE_DIR=$(/usr/bin/dirname "${RELATIVE_P}")
    echo -n "\
M${RELATIVE_P}\
=\
github.com/google/fleetspeak/${RELATIVE_DIR},"
  done
}

# These map proto import paths to Go import paths.
readonly PROTOC_M_OPTS=$(generate_protoc_m_opts)

readonly LICENSE_HEADER='# Copyright 2017 Google Inc.
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

'

/bin/echo '- Transpiling required protos to Go and Python.'

for source_package in ${PROTO_PACKAGES}; do
  /bin/echo "-- Transpiling ${source_package} to Go."
  # Pass the updated PATH to the PROTOC executable.
  PATH="${GOPATH}/bin/:${PATH}" \
  "${PROTOC}" \
    --go_out="plugins=grpc,${PROTOC_M_OPTS}:${FLEETSPEAK_PATH}" \
    --proto_path="${FLEETSPEAK_PATH}" \
    "${source_package}/"*'.proto'

  if [[ -f "${source_package}/__init__.py" ]]; then
    /bin/echo "-- Transpiling ${source_package} to Python."
    python \
      -m grpc_tools.protoc \
      --python_out="${FLEETSPEAK_PATH}" \
      --grpc_python_out="${FLEETSPEAK_PATH}" \
      --proto_path="${FLEETSPEAK_PATH}" \
      "${source_package}/"*'.proto'

    for generated_py in "${source_package}/"*_pb2{_grpc,}.py; do
      /bin/echo "${LICENSE_HEADER}$(/bin/cat "${generated_py}")" \
        > "${generated_py}"
    done
  fi
done
