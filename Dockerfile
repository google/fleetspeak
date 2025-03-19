FROM golang:1.22 AS builder

RUN apt-get update && \
    apt-get install -y  \
    python-is-python3

COPY . /fleetspeak

ENV GOBIN=/fleetspeak/bin
RUN mkdir -p $GOBIN
RUN cd /fleetspeak && go install ./...


FROM golang:1.22

RUN apt update

WORKDIR /

ENV FLEETSPEAK_BIN=/fleetspeak/bin
RUN mkdir -p $FLEETSPEAK_BIN

COPY --from=builder /fleetspeak/bin/fleetspeak_server $FLEETSPEAK_BIN/server
COPY --from=builder /fleetspeak/bin/fleetspeak_client $FLEETSPEAK_BIN/client
COPY --from=builder /fleetspeak/bin/fleetspeak_config $FLEETSPEAK_BIN
COPY --from=builder /fleetspeak/bin/fleetspeak_admin $FLEETSPEAK_BIN

ENV PATH="$FLEETSPEAK_BIN:$PATH"

ENTRYPOINT [ "server" ]
