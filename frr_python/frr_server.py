"""FRR Fleetspeak server

Receives messages from a client, prints them and forwards them to master server
"""

import logging
import time
import grpc

from absl import app
from absl import flags

from fleetspeak.server_connector.connector import InsecureGRPCServiceClient
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import TrafficResponseData
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import MessageInfo
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2_grpc import MasterStub


FLAGS = flags.FLAGS

flags.DEFINE_string(
    name="client_id",
    default="",
    help="An id of the client to send the messages to.")

channel = grpc.insecure_channel('localhost:6059')
stub = MasterStub(channel)

def Listener(message, context):
    """Receives a message from a client, prints it and forwards to master server."""

    del context  # Unused

    if message.message_type != "TrafficResponse":
        logging.info(f"Unknown message type: {message.message_type}")
        return

    response_data = TrafficResponseData()
    message.data.Unpack(response_data)
    logging.info(
        f"RESPONSE - master_id: {response_data.master_id}, "
        f"request_id: {response_data.request_id}, "
        f"response_index: {response_data.response_index}, "
        f"text: {response_data.data}")

    stub.RecordTrafficResponse(MessageInfo(client_id=message.source.client_id, data=response_data))


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("FRR")
    service_client.Listen(Listener)

    while True:
        time.sleep(1)

if __name__ == "__main__":
    app.run(main)
