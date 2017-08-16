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

"""This file defines the Fleetspeak API.

It exposes an interface to Fleetspeak's clients that lets them talk to their
servers.
"""

from fleetspeak.src.client.api.py import fd_comms_channel


_state = None


def Init():
  """Initializes the Fleetspeak client side API."""
  global _state
  if _state is not None:
    raise RuntimeError("Failed to initialize the Fleetspeak API, "
                       "fleetspeak.Init can't be called more than once.")

  _state = _ApiState()


def Send(api_message):
  """Sends a message to the server.

  Args:
    api_message: fleetspeak.client.api.APIMessage
  """
  _state.Send(api_message)


def Recv():
  """Receives a message from the server.

  Returns:
    fleetspeak.client.api.APIMessage
  """
  return _state.Recv()


class _ApiState(object):
  """Handles the state of Fleetspeak API."""

  def __init__(self):
    if not fd_comms_channel.FileDescriptorCommunicationChannelProvided():
      raise RuntimeError(
          "Failed to initialize the Fleetspeak API: Fleetspeak client "
          "environment not set up, note that Fleetspeak's fleetspeak.Init "
          "shouldn't be called by programs run outside of Fleetspeak.")

    self._impl = fd_comms_channel.FileDescriptorCommunicationChannelInitialize()

  def Send(self, api_message):
    """Sends a message to the server.

    Args:
      api_message: fleetspeak.client.api.APIMessage
    """
    self._impl.Send(api_message)

  def Recv(self):
    """Receives a message from the server."""
    return self._impl.Recv()
