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

import datetime
import threading
import time
from unittest import mock

from absl.testing import absltest
import grpc
import grpc_testing

from fleetspeak.server_connector import connector
from fleetspeak.src.common.proto.fleetspeak import common_pb2
from fleetspeak.src.server.proto.fleetspeak_server import admin_pb2
from fleetspeak.src.server.proto.fleetspeak_server import admin_pb2_grpc


class RetryLoopTest(absltest.TestCase):

  @mock.patch.object(time, "sleep")
  @mock.patch.object(time, "time", return_value=0)
  def testNotSleepingOnFirstSuccessfulCall(self, time_mock, sleep_mock):
    func = mock.Mock(return_value=42)

    result = connector.RetryLoop(
        func,
        timeout=datetime.timedelta(seconds=10.5),
        single_try_timeout=datetime.timedelta(seconds=1))

    func.assert_called_once()
    sleep_mock.assert_not_called()

    self.assertEqual(result, 42)

  @mock.patch.object(time, "sleep")
  @mock.patch.object(time, "time")
  def testSingleTryTimeoutIsUsedForCalls(self, time_mock, sleep_mock):
    cur_time = 0

    def SleepMock(v: float) -> None:
      nonlocal cur_time
      cur_time += v

    sleep_mock.side_effect = SleepMock
    time_mock.side_effect = lambda: cur_time

    def Func(timeout: datetime.timedelta) -> None:
      nonlocal cur_time
      cur_time += timeout.total_seconds()
      raise grpc.RpcError("error")

    func = mock.Mock(wraps=Func)

    with self.assertRaises(grpc.RpcError):
      connector.RetryLoop(func,
                          timeout=datetime.timedelta(seconds=10.5),
                          single_try_timeout=datetime.timedelta(seconds=1))

    # Expected timeline:
    # 0:    func(1)
    # 1:    sleep(1)
    # 2:    func(1)
    # 3:    sleep(2)
    # 5:    func(1):
    # 6:    sleep(4)
    # 10:   func(0.5)
    # 10.5: -> done
    self.assertListEqual(
        [c.args[0].total_seconds() for c in func.call_args_list],
        [1, 1, 1, 0.5])

  @mock.patch.object(time, "sleep")
  @mock.patch.object(time, "time")
  def testDefaultSingleTryTimeoutIsEqualToDefaultTimeout(
      self, time_mock, sleep_mock):
    cur_time = 0

    def SleepMock(v: float) -> None:
      nonlocal cur_time
      cur_time += v

    sleep_mock.side_effect = SleepMock
    time_mock.side_effect = lambda: cur_time

    def Func(timeout: datetime.timedelta) -> None:
      nonlocal cur_time
      cur_time += timeout.total_seconds()
      raise grpc.RpcError("error")

    func = mock.Mock(wraps=Func)

    with self.assertRaises(grpc.RpcError):
      connector.RetryLoop(func, timeout=datetime.timedelta(seconds=10))

    # Expected timeline:
    # 0:  func(10)
    # 10: -> done
    func.assert_called_once_with(datetime.timedelta(seconds=10))


class ClientTest(absltest.TestCase):

  def _fakeStub(self):
    return mock.create_autospec(
        admin_pb2_grpc.AdminStub(
            grpc_testing.channel(
                [],
                grpc_testing.strict_real_time(),
            )))

  def testKeepAlive(self):
    event = threading.Event()

    t = self._fakeStub()
    t.KeepAlive.side_effect = lambda *args, **kwargs: event.set()

    s = connector.OutgoingConnection(None, 'test', t)
    self.assertTrue(event.wait(10))

    s.Shutdown()

  def testInsertMessageIsDelegatedToStub(self):
    t = self._fakeStub()
    s = connector.OutgoingConnection(None, 'test', t)
    s.InsertMessage(common_pb2.Message())

    t.InsertMessage.assert_called_once()
    message = t.InsertMessage.call_args.args[0]
    self.assertEqual(message.source.service_name, 'test')

  def testInsertMessageIsRetried(self):
    t = self._fakeStub()
    t.InsertMessage.side_effect = [grpc.RpcError("error"), mock.DEFAULT]

    s = connector.OutgoingConnection(None, 'test', t)
    s.InsertMessage(common_pb2.Message())

    self.assertEqual(t.InsertMessage.call_count, 2)

  def testDeletePendingMessagesIsDelegatedToStub(self):
    t = self._fakeStub()
    s = connector.OutgoingConnection(None, 'test', t)
    s.DeletePendingMessages(admin_pb2.DeletePendingMessagesRequest())

    t.DeletePendingMessages.assert_called_once()

  def testDeletePendingMessagesIsRetried(self):
    t = self._fakeStub()
    t.DeletePendingMessages.side_effect = [
        grpc.RpcError("error"), mock.DEFAULT
    ]

    s = connector.OutgoingConnection(None, 'test', t)
    s.DeletePendingMessages(admin_pb2.DeletePendingMessagesRequest())

    self.assertEqual(t.DeletePendingMessages.call_count, 2)

  def testGetPendingMessagesIsDelegatedToStub(self):
    t = self._fakeStub()
    s = connector.OutgoingConnection(None, 'test', t)
    s.GetPendingMessages(admin_pb2.GetPendingMessagesRequest())

    t.GetPendingMessages.assert_called_once()

  def testGetPendingMessagesIsRetried(self):
    t = self._fakeStub()
    t.GetPendingMessages.side_effect = [grpc.RpcError("error"), mock.DEFAULT]

    s = connector.OutgoingConnection(None, 'test', t)
    s.GetPendingMessages(admin_pb2.GetPendingMessagesRequest())

    self.assertEqual(t.GetPendingMessages.call_count, 2)

  def testGetPendingMessageCountIsDelegatedToStub(self):
    t = self._fakeStub()
    s = connector.OutgoingConnection(None, 'test', t)
    s.GetPendingMessageCount(admin_pb2.GetPendingMessageCountRequest())

    t.GetPendingMessageCount.assert_called_once()

  def testGetPendingMessageCountIsRetried(self):
    t = self._fakeStub()
    t.GetPendingMessageCount.side_effect = [
        grpc.RpcError("error"), mock.DEFAULT
    ]

    s = connector.OutgoingConnection(None, 'test', t)
    s.GetPendingMessageCount(admin_pb2.GetPendingMessageCountRequest())

    self.assertEqual(t.GetPendingMessageCount.call_count, 2)


if __name__ == '__main__':
  absltest.main()
