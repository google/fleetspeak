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


/bin/echo >&2 ""
/bin/echo >&2 "Building server.deb"

fakeroot bash -c '
  rm -rf server-pkg
  cp -r server-pkg-tmpl server-pkg

  chmod 755 server-pkg/*

  sed -i "s/<version>/$(cat ../VERSION)/" server-pkg/DEBIAN/control

  mkdir -p server-pkg/usr/bin
  install -o root -g root src/server/server/server server-pkg/usr/bin/fleetspeak-server
  install -o root -g root src/config/fleetspeak_config server-pkg/usr/bin/fleetspeak-config
  
  dpkg-deb -b server-pkg server.deb
  dpkg-deb -c server.deb
'

/bin/echo >&2 ""
/bin/echo >&2 "Building client.deb"
fakeroot bash -c '
  rm -rf client-pkg
  cp -r client-pkg-tmpl client-pkg

  chmod 755 client-pkg/*

  sed -i "s/<version>/$(cat ../VERSION)/" client-pkg/DEBIAN/control

  mkdir -p client-pkg/usr/bin
  install -o root -g root src/client/client/client client-pkg/usr/bin/fleetspeak-client
  
  dpkg-deb -b client-pkg client.deb
  dpkg-deb -c client.deb
'



