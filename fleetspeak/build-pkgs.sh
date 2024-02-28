#!/bin/bash
# Copyright 2019 Google LLC.
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

set -e

/bin/echo >&2 ""
/bin/echo >&2 "Building binaries"

export BINDIR="$(mktemp -d)"
trap "rm -rf ${BINDIR}" EXIT

cd ..
CGO_ENABLED=0 GOBIN="${BINDIR}" go install ./cmd/...
cd -

/bin/echo >&2 ""
/bin/echo >&2 "Building server.deb"

export DEB_DEST="server-pkg/debian/fleetspeak-server"
export DEB_VERSION=$(cat ../VERSION)

fakeroot bash -c '
  set -e
  rm -rf server-pkg
  cp -r server-pkg-tmpl server-pkg

  chmod 755 server-pkg/*

  cd server-pkg
  debchange --create \
    --newversion "${DEB_VERSION}" \
    --package fleetspeak-server \
    --urgency low \
    --controlmaint \
    --distribution unstable \
    "Built by GitHub Actions at ${GITHUB_SHA}"
  cd -

  mkdir -p server-pkg/usr/bin
  install -o root -g root "${BINDIR}/fleetspeak_server" server-pkg/usr/bin/fleetspeak-server
  install -o root -g root "${BINDIR}/fleetspeak_config" server-pkg/usr/bin/fleetspeak-config
  install -o root -g root "${BINDIR}/fleetspeak_admin" server-pkg/usr/bin/fleetspeak-admin

  cd server-pkg
  dpkg-buildpackage -us -uc
  cd -
'

/bin/echo >&2 ""
/bin/echo >&2 "Building client.deb"
fakeroot bash -c '
  set -e
  rm -rf client-pkg
  cp -r client-pkg-tmpl client-pkg

  chmod 755 client-pkg/*

  cd client-pkg
  debchange --create \
    --newversion "${DEB_VERSION}" \
    --package fleetspeak-client \
    --urgency low \
    --controlmaint \
    --distribution unstable \
    "Built by GitHub Actions at ${GITHUB_SHA}"
  cd -

  mkdir -p client-pkg/usr/bin
  install -o root -g root "${BINDIR}/fleetspeak_client" client-pkg/usr/bin/fleetspeak-client

  cd client-pkg
  dpkg-buildpackage -us -uc
  cd -
'
