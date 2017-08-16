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

"""Utility methods for the Fleetspeak API."""

from fleetspeak.src.client.api.py import fleetspeak
from fleetspeak.src.client.proto.fleetspeak_client import api_pb2

_UNSPECIFIED = "UNSPECIFIED"


def PackMsg(msg, msg_type=_UNSPECIFIED):
  """Hides the proto boilerplate needed to pack an api_pb2.APIMessage.

  Args:
    msg: Arbitrary protobuf.
    msg_type: A string representation of the message type.

  Returns:
    api_pb2.APIMessage packed with the given message and type.
  """
  api_msg = api_pb2.APIMessage(type=msg_type)
  api_msg.data.Pack(msg)
  return api_msg


def UnpackMsg(api_msg, data_type, msg_type=None):
  """Hides the proto boilerplate needed to unpack an api_pb2.APIMessage.

  Args:
    api_msg: api_pb2.APIMessage to be unpacked.
    data_type: The protobuf type api_msg is to be unpacked into.
    msg_type: Optional string representation of the message type. If given, it
              will be ensured that api_msg's type field matches msg_type.

  Returns:
    data_type unpacked from the given message.
  """
  if msg_type is not None and api_msg.type != msg_type:
    raise TypeError("Unexpected APIMessage.type.")

  msg = data_type()
  api_msg.data.Unpack(msg)
  return msg


def SendMsg(msg, msg_type=_UNSPECIFIED):
  """Convenience shorthand for Send(PackMsg(...))."""
  fleetspeak.Send(PackMsg(msg, msg_type))


def RecvMsg(data_type, msg_type=None):
  """Convenience shorthand for UnpackMsg(Recv(), ...))."""
  return UnpackMsg(fleetspeak.Recv(), data_type, msg_type)
