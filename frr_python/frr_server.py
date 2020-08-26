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
    name="master_server_address",
    default="localhost:6059",
    help="Address of master server to forward clients' messages")


class Listener:
    """Connects to master server and processes messages from clients"""

    def __init__(self):
        channel = grpc.insecure_channel(FLAGS.master_server_address)
        self.stub = MasterStub(channel)

    def __call__(self, message, context):
        del context  # Unused

        if message.message_type != "TrafficResponse":
            logging.info("Unknown message type: %s", message.message_type)
            return

        response_data = TrafficResponseData()
        message.data.Unpack(response_data)
        logging.info(
            "RESPONSE - master_id: %d, "
            "request_id: %d, "
            "response_index: %d, "
            "text: %s",
            response_data.master_id,
            response_data.request_id,
            response_data.response_index,
            response_data.data)

        self.stub.RecordTrafficResponse(
            MessageInfo(client_id=message.source.client_id, data=response_data))


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("FRR")
    service_client.Listen(Listener())

    while True:
        time.sleep(1)

if __name__ == "__main__":
    app.run(main)
