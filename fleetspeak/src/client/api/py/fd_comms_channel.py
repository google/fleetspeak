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

"""This file implements the Fleetspeak file descriptor communication protocol.

The protocol uses interprocess pipes.

It's used by the Fleetspeak client processes to communicate with their parent
process, ie Fleetspeak.

The Python implementation of the protocol and the API in general is as close
as possible to its Go counterpart. Refer to the Go sources for detailed design
description.
"""

import os

from fleetspeak.src.client.proto.fleetspeak_client import api_pb2


def FileDescriptorCommunicationChannelProvided():
  """Checks the file descriptor communication channel is set up.

  Determines if the process's environment has a Fleetspeak file descriptor pipe
  set up.

  Returns:
    bool
  """
  return (
      (os.getenv("FLEETSPEAK_COMMS_CHANNEL_INFD") is not None) or
      (os.getenv("FLEETSPEAK_COMMS_CHANNEL_OUTFD") is not None))


def FileDescriptorCommunicationChannelInitialize():
  """Initializes the Fleetspeak client side API.

  This method initializes the API using a Fleetspeak file descriptor
  communication channel.

  Returns:
    A Fleetspeak API state implementation object.
  """
  in_file_descriptor = int(os.getenv("FLEETSPEAK_COMMS_CHANNEL_INFD"))
  pipe_read = os.fdopen(in_file_descriptor, "r")

  out_file_descriptor = int(os.getenv("FLEETSPEAK_COMMS_CHANNEL_OUTFD"))
  pipe_write = os.fdopen(out_file_descriptor, "w")

  return _FileDescriptorCommunicationApiAdapter(
      _FileDescriptorCommunicationChannel(pipe_read, pipe_write))


class _FileDescriptorCommunicationApiAdapter(object):
  """Implements the Fleetspeak client side API.

  Uses a Fleetspeak file descriptor communication channel to do so.
  """

  def __init__(self, file_descriptor_comms_channel):
    self._file_descriptor_comms_channel = file_descriptor_comms_channel

  def Send(self, api_message):
    """Sends a message to the server.

    Args:
      api_message: fleetspeak.client.api.APIMessage
    """
    self._file_descriptor_comms_channel.Send(api_message.SerializeToString())

  def Recv(self):
    """Receives a message from the server.

    Returns:
      fleetspeak.client.api.APIMessage
    """
    api_message = api_pb2.APIMessage()
    api_message.ParseFromString(self._file_descriptor_comms_channel.Recv())
    return api_message


class _FileDescriptorCommunicationChannel(object):
  """Implements the Fleetspeak file descriptor communication channel."""

  _HEADER = "Fleetspeak comms channel\n"
  _LENGTH_BYTES = 10
  _LENGTH_FORMAT = "%" + str(_LENGTH_BYTES) + "d"
  _LENGTH_LIMIT = 10**_LENGTH_BYTES

  def __init__(self, pipe_read, pipe_write):
    self._pipe_read = pipe_read
    self._pipe_write = pipe_write

    self._ReadInitialize()
    self._WriteInitialize()

  def Recv(self):
    """Receives a message from the channel.

    Returns:
      string
    """
    l = int(self._ReadN(_FileDescriptorCommunicationChannel._LENGTH_BYTES))
    return self._ReadN(l)

  def Send(self, message):
    """Sends a message through the channel.

    Args:
      message: string

    Raises:
      ValueError: If message is longer than _LENGTH_LIMIT.
    """
    if len(message) >= _FileDescriptorCommunicationChannel._LENGTH_LIMIT:
      raise ValueError(
          "Can't send a message longer than %d." %
          _FileDescriptorCommunicationChannel._LENGTH_LIMIT)

    message_length = _FileDescriptorCommunicationChannel._LENGTH_FORMAT % (
        len(message))
    self._pipe_write.write(message_length)
    self._pipe_write.write(message)
    self._pipe_write.flush()

  def _ReadInitialize(self):
    header_buffer = self._ReadN(
        len(_FileDescriptorCommunicationChannel._HEADER))
    if header_buffer != _FileDescriptorCommunicationChannel._HEADER:
      raise ValueError("Received an unexpected header.")

  def _WriteInitialize(self):
    self._pipe_write.write(_FileDescriptorCommunicationChannel._HEADER)
    self._pipe_write.flush()

  def _ReadN(self, n):
    return _ReadN(self._pipe_read, n)


def _ReadN(file_handle, n):
  """Reads n characters from file_handle, or until EOF.

  This is equivalent to the current CPython implementation of
  file_handle.read(n), but it's not guaranteed by the docs.

  Args:
    file_handle: file
    n: int

  Returns:
    string
  """
  ret = ""
  while True:
    chunk = file_handle.read(n - len(ret))
    ret += chunk

    if len(ret) == n or not chunk:
      return ret
