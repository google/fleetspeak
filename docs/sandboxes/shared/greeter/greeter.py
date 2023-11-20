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
import binascii
import logging

from absl import app
from absl import flags
from fleetspeak.server_connector.connector import InsecureGRPCServiceClient
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from google.protobuf.wrappers_pb2 import StringValue


FLAGS = flags.FLAGS

flags.DEFINE_string(
    name="client_id",
    default="",
    help="An id of the client to send the messages to.")


def listener(message, context):
    del context  # Unused

    data = StringValue()
    message.data.Unpack(data)
    logging.info(f"RESPONSE: {data.value}")


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("greeter")
    service_client.Listen(listener)

    while True:
        data = StringValue()
        data.value = input("Enter your name: ")

        request = Message()
        request.destination.client_id = binascii.unhexlify(FLAGS.client_id)
        request.destination.service_name = "hello"
        request.data.Pack(data)

        service_client.Send(request)


if __name__ == "__main__":
    app.run(main)
