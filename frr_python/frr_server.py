"""FRR Fleetspeak server

Sends TrafficRequestData messages to a client and receives back
TrafficResponseData messages
"""

import binascii
import logging
import time

from absl import app
from absl import flags

from fleetspeak.server_connector.connector import InsecureGRPCServiceClient
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import TrafficRequestData
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import TrafficResponseData


FLAGS = flags.FLAGS

flags.DEFINE_string(
    name="client_id",
    default="",
    help="An id of the client to send the messages to.")


def Listener(message, context):
    """Receives a message from a client and prints it."""

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


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("FRR_server")
    service_client.Listen(Listener)

    current_id = 0

    for _ in range(5):
        request_data = TrafficRequestData(
            master_id=0,
            request_id=current_id,
        )
        current_id += 1

        request = Message()
        request.destination.client_id = binascii.unhexlify(FLAGS.client_id)
        request.destination.service_name = "FRR_client"
        request.data.Pack(request_data)
        request.message_type = "TrafficRequest"

        service_client.Send(request)
        time.sleep(3)


if __name__ == "__main__":
    app.run(main)
