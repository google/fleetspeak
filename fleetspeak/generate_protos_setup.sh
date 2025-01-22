#!/bin/bash
set -e

PROTOC_VER="26.1" # https://github.com/protocolbuffers/protobuf/releases
GEN_GO_VER="1.34.1" # https://pkg.go.dev/google.golang.org/protobuf/cmd/protoc-gen-go?tab=versions
GEN_GO_GRPC_VER="1.4.0" # https://pkg.go.dev/google.golang.org/grpc/cmd/protoc-gen-go-grpc?tab=versions

function err() {
  echo "$*" >&2
}

# Installs protoc from https://github.com/protocolbuffers/protobuf/releases to
# ~/.local/bin. Well known proto types are placed in ~/.local/include.
# Globals:
#   PROTOC_VER
# Arguments:
#   None
function install_protoc() {
  local temp_zip
  temp_zip="$(mktemp)"
  trap "rm -f ${temp_zip}" EXIT
  curl -L "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VER}/protoc-${PROTOC_VER}-linux-x86_64.zip" -o "${temp_zip}"
  unzip -o "${temp_zip}" -x readme.txt -d "${HOME}/.local"
  echo "Installed protoc v${PROTOC_VER}. Ensure ${HOME}/.local/bin is in your PATH."
}

if [[ "$(protoc --version)" != "libprotoc ${PROTOC_VER}" ]]; then
  if [[ "$1" == "check" ]]; then
    err "Wrong protobuf version, please run fleetspeak/generate_protos_setup.sh"
    exit 1
  fi
  install_protoc
fi

if [[ "$(protoc-gen-go --version)" != "protoc-gen-go v${GEN_GO_VER}" ]]; then
  if [[ "$1" == "check" ]]; then
    err "Wrong protoc-gen-go version, please run fleetspeak/generate_protos_setup.sh"
    exit 1
  fi
  go install "google.golang.org/protobuf/cmd/protoc-gen-go@v${GEN_GO_VER}"
fi

if [[ "$(protoc-gen-go-grpc --version)" != "protoc-gen-go-grpc ${GEN_GO_GRPC_VER}" ]]; then
  if [[ "$1" == "check" ]]; then
    err "Wrong protoc-gen-go-grpc version, please run fleetspeak/generate_protos_setup.sh"
    exit 1
  fi
  go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${GEN_GO_GRPC_VER}"
fi
