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

"""Tests for grpcservice.client.client."""

import threading

import unittest
from fleetspeak.src.server.grpcservice.client import client


class FakeStub(object):

  def __init__(self):
    self.event = threading.Event()

  def KeepAlive(self, unused, timeout=None):
    del unused
    del timeout
    self.event.set()


class ClientTest(unittest.TestCase):

  def testKeepAlive(self):
    t = FakeStub()
    s = client.Sender(None, 'test', t)
    self.assertTrue(t.event.wait(10))
    s.Shutdown()


if __name__ == '__main__':
  unittest.main()
