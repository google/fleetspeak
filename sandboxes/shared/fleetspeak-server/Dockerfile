# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
FROM golang:1.22 as builder

RUN apt update && \
    apt install -y python3-venv && \
    apt install -y pip && \
    apt install -y git

WORKDIR /

SHELL ["/bin/bash", "-c"]

RUN git clone https://github.com/google/fleetspeak.git && \
    cd fleetspeak && \
    go get -u golang.org/x/lint/golint && \
    ./fleetspeak/generate_protos_setup.sh

ENV PATH="$HOME/.local/bin:$PATH"

ENV GOBIN=/fleetspeak-bin

RUN mkdir -p ${GOBIN}

RUN cd /fleetspeak && \
    python3 -m venv $HOME/.venv/FSENV && \
    source $HOME/.venv/FSENV/bin/activate && \
    pip install wheel pytest && \
    pip install -e ./fleetspeak_python[test] && \
    pip install -e ./frr_python && \
    ./fleetspeak/generate_protos.sh && \
    go install ./cmd/fleetspeak_server ./cmd/fleetspeak_client \
        ./cmd/fleetspeak_config

FROM golang:1.22

RUN mkdir -p /app/bin

COPY --from=builder /fleetspeak-bin/fleetspeak_server /app/bin/server
COPY --from=builder /fleetspeak-bin/fleetspeak_client /app/bin/client
COPY --from=builder /fleetspeak-bin/fleetspeak_config /app/bin/
