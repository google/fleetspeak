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

"""A simple GRPCService based loopback.

This script uses the GRPCService client library to receive messages from
a fleetspeak server. They are returned to the sending address, again
using the GRPCService client library.
"""

import logging
import threading
import time

from absl import app
from absl import flags

from fleetspeak.server_connector import connector

FLAGS = flags.FLAGS


def main(argv=None):
  del argv  # Unused.

  service_client = connector.InsecureGRPCServiceClient("TestService")
  seen = set()
  seen_lock = threading.Lock()

  def loop(message, context):
    """loop a message back to fleetspeak."""
    del context  # Unused

    logging.info("Received message.")
    with seen_lock:
      if message.message_id in seen:
        logging.warning("Ignoring duplicate.")
        return
      seen.add(message.message_id)

    message.ClearField("source_message_id")
    message.ClearField("message_id")
    message.destination.service_name = message.source.service_name
    message.destination.client_id = message.source.client_id

    service_client.Send(message)
    logging.info("Sent message.")

  service_client.Listen(loop)
  while True:
    time.sleep(600)


if __name__ == "__main__":
  app.run(main)
