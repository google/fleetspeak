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

from google.apputils import app
import gflags
import logging

import fleetspeak.src.client.daemonservice.client.client as clib

FLAGS = gflags.FLAGS
gflags.DEFINE_string("mode", "loopback",
                     "Mode of operation. Options are: loopback")


class FatalError(Exception):
  pass


def Loopback():
  logging.info("starting loopback")
  con = clib.FleetspeakConnection(version="0.5")
  logging.info("connection created")
  while True:
    msg, _ = con.Recv()
    msg.message_type += "Response"
    con.Send(msg)


def main(argv=None):
  del argv  # Unused.

  if FLAGS.mode == "loopback":
    Loopback()
    return
  raise FatalError("Unknown mode: %s", FLAGS.mode)


if __name__ == "__main__":
  app.run()
