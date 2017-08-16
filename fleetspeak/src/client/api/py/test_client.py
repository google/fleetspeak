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

"""Test client using FS TCP gRPC API."""

from fleetspeak.src.client.api.py import fleetspeak
from fleetspeak.src.client.api.py import fleetspeak_utils
from fleetspeak.src.client.proto.fleetspeak_client import api_pb2

if __name__ == "__main__":
  fleetspeak.Init()

  while True:
    msg_data = fleetspeak_utils.RecvMsg(api_pb2.ByteBlob, "ByteBlob")

    fleetspeak_utils.SendMsg(
        api_pb2.ByteBlob(data="Received: %s; Hello server!" % msg_data.data),
        "ByteBlob")
