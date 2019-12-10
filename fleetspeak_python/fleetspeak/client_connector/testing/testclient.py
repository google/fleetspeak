#!/usr/bin/env python
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

"""Simplified python testclient for Fleetspeak daemonservice.

This is a minimal daemonservice client. It is started and exercised by
daemonservice_test.go in order to verify that the python client library
correctly implements the protocol expected by daemonservice.
"""

import logging
import time

from absl import app
from absl import flags

from fleetspeak.client_connector import connector

FLAGS = flags.FLAGS
flags.DEFINE_string("mode", "loopback",
                    "Mode of operation. Options are: loopback")


class FatalError(Exception):
  pass


def Loopback():
  logging.info("starting loopback")
  con = connector.FleetspeakConnection(version="0.5")
  logging.info("connection created")
  while True:
    msg, _ = con.Recv()
    msg.message_type += "Response"
    con.Send(msg)


def MemoryHog():
  """Takes 20MB of memory, then sleeps forever."""
  logging.info("starting memory leak")
  con = connector.FleetspeakConnection(version="0.5")
  logging.info("connection created")
  buf = "a" * (1024*1024*20)
  while True:
    time.sleep(1)


def Freezed():
  """Connects to Fleetspeak, then sleeps indefinitely."""
  logging.info("starting freezed")
  con = connector.FleetspeakConnection(version="0.5")
  logging.info("connection created")
  while True:
    time.sleep(1)


def Heartbeat():
  """Sends a heartbeat every second, indefinitely."""
  logging.info("starting heartbeat")
  con = connector.FleetspeakConnection(version="0.5")
  logging.info("connection created")
  while True:
    time.sleep(1)
    con.Heartbeat()


def main(argv=None):
  del argv  # Unused.

  if FLAGS.mode == "loopback":
    Loopback()
    return

  if FLAGS.mode == "memoryhog":
    MemoryHog()
    return

  if FLAGS.mode == "freezed":
    Freezed()
    return

  if FLAGS.mode == "heartbeat":
    Heartbeat()
    return

  raise FatalError("Unknown mode: %s", FLAGS.mode)


if __name__ == "__main__":
  app.run(main)
