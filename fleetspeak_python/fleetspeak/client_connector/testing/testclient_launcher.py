#!/usr/bin/env python
# Copyright 2018 Google Inc.
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

"""Script that launches testclient.Loopback in another process."""

import subprocess

from absl import app

def main(argv=None):
  del argv
  p = subprocess.Popen(
    [
      "python",
      "-m",
      "fleetspeak.client_connector.testing.testclient",
      "--",
      "--mode=loopback"
    ],
    # Make sure file descriptors passed from the parent Fleetspeak process are
    # not closed. This is critical for inter-process communication between
    # the Fleetspeak client and the test client.
    close_fds=False)
  p.communicate()
  p.wait()


if __name__ == "__main__":
  app.run(main)
