# End-to-End testing framework design

## FRR

The
[FRR service](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr/frr.proto)
is a dummy/mock fleetspeak service meant for load/integration testing. It
consists of a protocol specification and a GO implementation of a FRR client and
server. Multiple FRR clients and servers can run in-process with a fleetspeak
client and server. The GO FRR implementation is optimized for load testing - to
put as much load on the database as possible. However, one focus of this project
is correct operation of clients/servers running in separate processes and
testing of the python connector libraries.

In this project: - FRR protocol is reused. - A new
[python implementation](https://github.com/google/fleetspeak/tree/master/frr_python)
of a subset of the FRR server and client is implemented. - The implementation is
using the connector libraries and running in separate processes.

The following is implemented: 1.
[frr_client.py](https://github.com/google/fleetspeak/blob/master/frr_python/frr_client.py) -
Runs in a separate process. - Uses the
[client connector](https://github.com/google/fleetspeak/tree/master/fleetspeak_python/fleetspeak/client_connector)
as an interface to fleetspeak. - Receives fleetspeak messages. - If the message
type equals "TrafficRequest", deserializes the payload as an
[TrafficRequestData](https://github.com/google/fleetspeak/blob/690991be00993813230a8f6c3aad703b21dfb0c5/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr/frr.proto#L15). -
Replies with an appropriate
[TrafficResponseData](https://github.com/google/fleetspeak/blob/690991be00993813230a8f6c3aad703b21dfb0c5/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr/frr.proto#L36).

1.  [frr_server.py](https://github.com/google/fleetspeak/blob/master/frr_python/frr_server.py)
2.  Runs in a separate process.
3.  Has a GRPC connection to the FRR master server.
4.  Uses the
    [server connector](https://github.com/google/fleetspeak/tree/master/fleetspeak_python/fleetspeak/server_connector)
    as an interface to fleetspeak.
5.  Receives a fleetspeak message.
6.  If the message type equals "TrafficResponse", deserializes the payload as
    TrafficResponseData.
7.  Forwards the received TrafficResponseData to the FRR master server.

8.  [frr_master_server_main.go](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/e2etesting/frr-master-server-main/frr_master_server_main.go)

9.  [frr.go](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/inttesting/frr/frr.go)
    contains the logic to run a FRR master server, however the logic can't be
    run stand-alone, it doesn't have its own main(). frr_master_server_main.go
    is created to run the FRR server as a standalone binary.

Extensions to the FRR master server: - The Master service in
[frr.proto](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr/frr.proto)
is extended. - A new rpc CompletedRequests() is added. This rpc exposes the
respective
[implementation](https://github.com/google/fleetspeak/blob/690991be00993813230a8f6c3aad703b21dfb0c5/fleetspeak/src/inttesting/frr/frr.go#L454)
as an RPC method. - A new rpc CreateHunt() is added. It exposes the
[respective function](https://github.com/google/fleetspeak/blob/690991be00993813230a8f6c3aad703b21dfb0c5/fleetspeak/src/inttesting/frr/frr.go#L542)
and sends either broadcast request or unicast messages to all the clients.

## Test cases

The system supports multiple test cases. However, the main focus of the project
is a single test case (the end-to-end test service "basically working") executed
in multiple configurations. Tests can be run with a local installation (all the
components are started in separate processes on one machine) and with a
[distributed installation](https://github.com/google/fleetspeak/tree/master/terraform).

## Test framework

The job of the test framework is to set up a working fleetspeak + FRR testing
environment. In particular: - It runs the fleetspeak-config binary to generate
configuration files for servers and clients. - It generates service
configuration files for the FRR service. - It starts the fleetspeak clients,
servers, the FRR python binaries, the Load balancer and the Master server as
separate processes. - It provides functionality to stop all the processes it
started.

## Load balancer

A simple
[load balancer](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/e2etesting/balancer/balancer.go)
was implemented for the local testing. It picks a random active server and
forwards client's messages to it. The load balancer implements
[PROXY protocol Version 1](https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt).

## Local testing

Testing framework is used for the local testing. Local testing is added to CI.
To run the tests on your machine you have to create fleetspeak database and
associated user:

Run mysql console: `$ mysql --user root` Create database and user: `mysql>
CREATE USER fs_user IDENTIFIED BY "fs_password"; mysql> CREATE DATABASE
fs_test_db; mysql> GRANT ALL PRIVILEGES ON fs_test_db.* TO fs_user;` Exit mysql
and set MySQL parameters in environment variables: `export
MYSQL_TEST_USER="fs_user" export MYSQL_TEST_PASS="fs_password" export
MYSQL_TEST_ADDR="127.0.0.1:3306" export MYSQL_TEST_E2E_DB="fs_test_db"` `cd` to
[fleetspeak/src/e2etesting/localtesting](https://github.com/google/fleetspeak/tree/master/fleetspeak/src/e2etesting/localtesting)
and run `go test`

or

Run all the tests by running `fleetspeak/test.sh`. This script will also run the
local end-to-end tests.

## Cloud testing

The testing framework is also used to run the tests with distributed fleetspeak
installation in a cloud. The code and a guide how to test a distributed
installation using Terraform can be found in
[terraform directory](https://github.com/google/fleetspeak/tree/master/terraform).
