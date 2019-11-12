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
"""A client library for fleetspeak daemonservices.

This library is for use by a process run by the Fleetspeak daemonservice module
to send and receive messages.  The low level protocol is described in
daemonservice/channel/channel.go.
"""

import io
import os
import platform
import struct
import threading

from fleetspeak.src.client.channel.proto.fleetspeak_channel import channel_pb2
from fleetspeak.src.common.proto.fleetspeak import common_pb2

_WINDOWS = (platform.system() == "Windows")
if _WINDOWS:
  import msvcrt  # pylint: disable=g-import-not-at-top


class ProtocolError(Exception):
  """Raised when we do not understand the data received from Fleetspeak."""


# Constants to match behavior of channel.go.
_MAGIC = 0xf1ee1001

# We recommend that messages be ~1MB or smaller, and daemonservice has has 2MB
# hardcoded maximum.
MAX_SIZE = 2 * 1024 * 1024

# Format for the struct module to pack/unpack a 32 bit unsigned integer to/from
# a little endian byte sequence.
_STRUCT_FMT = "<I"

# The number of bytes required/produced when using _STRUCT_FMT.
_STRUCT_LEN = 4

# Environment variables, used to find the filedescriptors left open for
# us when started by Fleetspeak.
_INFD_VAR = "FLEETSPEAK_COMMS_CHANNEL_INFD"
_OUTFD_VAR = "FLEETSPEAK_COMMS_CHANNEL_OUTFD"


def _EnvOpen(var, mode):
  """Open a file descriptor identified by an environment variable."""
  value = os.getenv(var)
  if value is None:
    raise ValueError("%s is not set" % var)

  fd = int(value)

  # If running on Windows, convert the file handle to a C file descriptor; see:
  # https://groups.google.com/forum/#!topic/dev-python/GeN5bFJWfJ4
  if _WINDOWS:
    fd = msvcrt.open_osfhandle(fd, 0)

  return io.open(fd, mode)


class FleetspeakConnection(object):
  """A connection to the Fleetspeak system.

  It's safe to call methods of this class in parallel.
  """

  def __init__(self, version=None, read_file=None, write_file=None):
    """Connect to Fleetspeak.

    Connects to and begins an initial exchange of magic numbers with the
    Fleetspeak process. In normal use, the arguments are not required and will
    be created using the environment variables set by daemonservice.

    Args:

      version: A string identifying the version of the service being run. Will
        be included in resource reports for this service.

      read_file: A python file object, or similar, used to read bytes from
        Fleetspeak. If None, will be created based on the execution environment
        provided by daemonservice.

      write_file: A python file object, or similar, used to write bytes to
        Fleetspeak. If None, will be created based on the execution environment
        provided by daemonservice.

    Raises:
      ValueError: If read_file and write_file are not provided, and the
        corresponding environment variables are not set.
      ProtocolError: If we receive unexpected data from Fleetspeak.
    """
    self._read_file = read_file
    if not self._read_file:
      self._read_file = _EnvOpen(_INFD_VAR, "rb")

    self._read_lock = threading.Lock()

    self._write_file = write_file
    if not self._write_file:
      self._write_file = _EnvOpen(_OUTFD_VAR, "wb")

    self._write_lock = threading.Lock()

    # It is safer to send the magic number before reading it, in case the other
    # end does the same. Also, we'll be killed as unresponsive if we don't
    # write the magic number quickly enough. (Currently though, the other end is
    # the go implementation, which reads and writes in parallel.)
    self._WriteMagic()

    self._WriteStartupData(version)
    self._ReadMagic()

  def Send(self, message):
    """Send a message through Fleetspeak.

    Args:
      message: A message protocol buffer.
    Returns:
      Size of the message in bytes.
    Raises:
      ValueError: If message is not a common_pb2.Message.
    """
    if not isinstance(message, common_pb2.Message):
      raise ValueError("Send requires a fleetspeak.Message")

    if message.destination.service_name == "system":
      raise ValueError(
          "Only predefined messages can have destination.service_name == \"system\"")

    return self._SendImpl(message)

  def _SendImpl(self, message):
    if not isinstance(message, common_pb2.Message):
      raise ValueError("Send requires a fleetspeak.Message")

    buf = message.SerializeToString()
    if len(buf) > MAX_SIZE:
      raise ValueError(
          "Serialized message too large, size must be at most %d, got %d" %
          (MAX_SIZE, len(buf)))

    with self._write_lock:
      self._write_file.write(struct.pack(_STRUCT_FMT, len(buf)))
      self._write_file.write(buf)
      self._WriteMagic()

    return len(buf)

  def Recv(self):
    """Accept a message from Fleetspeak.

    Returns:
      A tuple (common_pb2.Message, size of the message in bytes).
    Raises:
      ProtocolError: If we receive unexpected data from Fleetspeak.
    """
    size = struct.unpack(_STRUCT_FMT, self._ReadN(_STRUCT_LEN))[0]
    if size > MAX_SIZE:
      raise ProtocolError("Expected size to be at most %d, got %d" % (MAX_SIZE,
                                                                      size))
    with self._read_lock:
      buf = self._ReadN(size)
      self._ReadMagic()

    res = common_pb2.Message()
    res.ParseFromString(buf)

    return res, len(buf)

  def Heartbeat(self):
    """Sends a heartbeat to the Fleetspeak client.

    If this daemonservice is configured to use heartbeats, clients that don't
    call this method often enough are considered faulty and are restarted by
    Fleetspeak.
    """
    heartbeat_msg = common_pb2.Message(
        message_type="Heartbeat",
        destination=common_pb2.Address(service_name="system"))
    self._SendImpl(heartbeat_msg)

  def _ReadMagic(self):
    got = struct.unpack(_STRUCT_FMT, self._ReadN(_STRUCT_LEN))[0]
    if got != _MAGIC:
      raise ProtocolError("Expected to read magic number {}, got {}.".format(
          _MAGIC, got))

  def _WriteMagic(self):
    buf = struct.pack(_STRUCT_FMT, _MAGIC)
    self._write_file.write(buf)
    self._write_file.flush()

  def _WriteStartupData(self, version):
    startup_msg = common_pb2.Message(
        message_type="StartupData",
        destination=common_pb2.Address(service_name="system"))
    startup_msg.data.Pack(
        channel_pb2.StartupData(pid=os.getpid(), version=version))
    self._SendImpl(startup_msg)

  def _ReadN(self, n):
    """Reads n characters from the input stream, or until EOF.

    This is equivalent to the current CPython implementation of read(n), but
    not guaranteed by the docs.

    Args:
      n: int

    Returns:
      string
    """
    ret = b""
    while True:
      chunk = self._read_file.read(n - len(ret))
      ret += chunk

      if len(ret) == n or not chunk:
        return ret
