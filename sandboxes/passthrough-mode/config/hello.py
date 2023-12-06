# Copyright 2023 Google Inc.
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

from absl import app
from fleetspeak.client_connector.connector import FleetspeakConnection
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from google.protobuf.wrappers_pb2 import StringValue


def main(argv):
  del argv  # Unused.

  conn = FleetspeakConnection(version="0.0.1")
  while True:
    request, _ = conn.Recv()

    data = StringValue()
    request.data.Unpack(data)

    data.value = f"Hello {data.value}!"

    response = Message()
    response.destination.service_name = request.source.service_name
    response.data.Pack(data)

    conn.Send(response)


if __name__ == "__main__":
  app.run(main)
