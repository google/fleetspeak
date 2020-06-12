"""Starts Fleetspeak client

Receives TrafficRequestData messages from server service and responses with
TrafficResponseData
"""

from absl import app
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from fleetspeak.client_connector.connector import FleetspeakConnection
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import TrafficRequestData
from fleetspeak.src.inttesting.frr.proto.fleetspeak_frr.frr_pb2 import TrafficResponseData

def main(argv):
    del argv  # Unused.

    conn = FleetspeakConnection(version="0.0.1")
    while True:
        request, _ = conn.Recv()
        if request.message_type != "TrafficRequest":
            continue

        rec_data = TrafficRequestData()
        request.data.Unpack(rec_data)

        resp_data = TrafficResponseData(
            master_id=rec_data.master_id,
            request_id=rec_data.request_id,
            response_index=0,
            data=b"client response",
            fin=True)

        response = Message()
        response.destination.service_name = request.source.service_name
        response.data.Pack(resp_data)
        response.message_type = "TrafficResponse"

        conn.Send(response)


if __name__ == "__main__":
    app.run(main)
