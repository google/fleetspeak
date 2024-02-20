FROM golang:1.22 as builder

RUN apt-get update && \
    apt-get install -y  \
    python-is-python3

COPY . /fleetspeak

RUN cd /fleetspeak && ./fleetspeak/build.sh


FROM golang:1.22

RUN apt update

WORKDIR /

ENV FLEETSPEAK_BIN /fleetspeak/bin
RUN mkdir -p $FLEETSPEAK_BIN

COPY --from=builder /fleetspeak/fleetspeak/src/server/server/server $FLEETSPEAK_BIN
COPY --from=builder /fleetspeak/fleetspeak/src/client/client/client $FLEETSPEAK_BIN
COPY --from=builder /fleetspeak/fleetspeak/src/config/fleetspeak_config $FLEETSPEAK_BIN
COPY --from=builder /fleetspeak/fleetspeak/src/admin/fleetspeak_admin $FLEETSPEAK_BIN


ENV PATH="$FLEETSPEAK_BIN:$PATH"

ENTRYPOINT [ "server" ]
