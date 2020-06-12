"""A Fleetspeak server service

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


def listener(message, context):
    """Receives a message from a client and prints it."""

    del context  # Unused

    if message.message_type != "TrafficResponse":
        logging.info(f"Unknown message type: {message.message_type}")
        return

    resp_data = TrafficResponseData()
    message.data.Unpack(resp_data)
    logging.info(
        f"RESPONSE - master_id: {resp_data.master_id}, "
        f"request_id: {resp_data.request_id}, "
        f"response_index: {resp_data.response_index}, "
        f"text: {resp_data.data}")


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("FRR_server")
    service_client.Listen(listener)

    current_id = 0

    for _ in range(5):
        req_data = TrafficRequestData(
            master_id=0,
            request_id=current_id,
        )
        current_id += 1

        request = Message()
        request.destination.client_id = binascii.unhexlify(FLAGS.client_id)
        request.destination.service_name = "FRR_client"
        request.data.Pack(req_data)
        request.message_type = "TrafficRequest"

        service_client.Send(request)
        time.sleep(3)


if __name__ == "__main__":
    app.run(main)
