"""FRR Fleetspeak client

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

    connection = FleetspeakConnection(version="0.0.1")
    while True:
        request, _ = connection.Recv()
        if request.message_type != "TrafficRequest":
            continue

        request_data = TrafficRequestData()
        request.data.Unpack(request_data)

        response_data = TrafficResponseData(
            master_id=request_data.master_id,
            request_id=request_data.request_id,
            response_index=0,
            data=b"client response",
            fin=True)

        response = Message()
        response.destination.service_name = request.source.service_name
        response.data.Pack(response_data)
        response.message_type = "TrafficResponse"

        connection.Send(response)


if __name__ == "__main__":
    app.run(main)
