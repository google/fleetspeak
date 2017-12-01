# Fleetspeak Design

Fleetspeak is a client-server framework for communicating with a fleet of
machines. This document summarizes the various components, providing an overview
of what bits serve what purpose.

## Identity

Fleetspeak identifies clients using a fingerprint of a public key, much like
openssl keys. Specifically, client creates a keypair on startup, or when asked
to rekey. The Fleetspeak
[`common.ClientID`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/common#ClientID)
is the first 8 bytes of a sha256 hash of the public part of this key. It is the
[Server Communicator's](#server-communicator) responsibility to verify the identity of
clients. The recommended approach is to communicate over TLS using the client's
Fleetspeak key as TLS client identification. The [`https.Communicator`
source](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/server/https/https.go)
has an example of this.

## Messages

At its core, Fleetspeak forwards
[`fleetspeak.Message`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/common/proto/fleetspeak/common.proto)
protocol buffers. The comments in the `.proto` file should be considered
definitive, but the most important fields to understand are `destination`,
`message_type` and `data`. Essentially, `destination` field tells Fleetspeak
where to forward the message to. The `message_type` and `data` fields are
forwarded verbatim, and when integrating a service a developer should set these
fields according to their needs.

The `message_type` is meant to be used both for dispatching within a service,
and for reporting. So for example if your integration has three basic types of
messages to pass, you might indicate them with three different message type
strings. By doing this you make it possible to query Fleetspeak for the number
of messages of each type.

# Server

A Fleetspeak Server middleware process written in go. To start a server, you
instantiate a
[`server.Server`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server#Server),
providing the components needed for your particular installation.


## Miniserver: An Example
The
[`miniserver`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/demo/miniserver/miniserver.go)
binary from the demo directory defines a Fleetspeak Server in a way suitable for
demonstrations and small installations.

When run, the miniserver binary binds two ports, and accepts bind addresses for
each from the command line. The `--https_addr` flag determines the interface and
port that clients will connect to. It needs to be open to the internet, or at
least to all Fleetspeak client machines. See [Server
Communicator](#server-communicator), below.

The `--admin_addr` flag determines the interface and port that the
[Administrative Interface](#administrative-interface) listens on. The miniserver
binary does not perform any authentication of administrative requests. Therefore
access to this port needs to be limited to trusted processes.

The miniserver process stores all of its state in an SQLite version 3
database. The location of this database file is set by the flag
`--database_path`. This file will be created if missing. After running
miniserver, you can examine the system state using, e.g., the `sqlite3` command
and package on Debian systems. See [Datastore](#datastore) below.

## Administrative Interface

The Administrative Interface is a [GRPC](https://grpc.io/) server which provides
access to Fleetspeak state for administrative and integration purposes. It is
implemented separately from the Fleetspeak Server and can be run from the same
or a different process, so long as it is connected to the same
[Datastore](#datastore)

## Datastore

The
[`db.Store`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/db#Store)
interface defines the persistent state held by a Fleetspeak Server. If a
Fleetspeak installation has multiple server processes, they should run the same
Datastore implementation providing a view of the same database. Currently we
provide two Datastore implementations.

At a minimum, any Datastore implementation should pass the tests implemented by
the [`dbtesting`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/dbtesting)
package.

### SQLite

The
[`sqlite.Datastore`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/sqlite#Datastore)
implements
[`db.Store`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/db#Store)
backed by an SQLite version 3 database. This implementation is used extensively
in our unit tests, and its design choices focus on simplicity, safety and
ease-of-use rather than performance. It is meant for testing, local
demonstrations, and single server installations with a small number of clients.

It does not support multiple servers processes - the SQLite database file should
only be opened by one instance of `sqlite.Datastore` at a time.

### Mysql
The
[`mysql.Datastore`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/mysql#Datastore)
implements
[`db.Store`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/db#Store)
backed by a mysql database. Using recent versions of the
[`go-sql-driver/mysql`](https://github.com/go-sql-driver/mysql) driver it passes
[`dbtesting`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/dbtesting),
but performance testing and database optimization is still in progress.

## Server Communicator

Every server requires a
[`Communicator`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/comms#Communicator)
component which handles communication with clients. This component defines how
the server communicates with fleetspeak machines and should agree on protocol
with a corresponding [Client Communicator](#client-communicator).

The quintessential example is
[`https.Communicator`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/https#Communicator)
and at this point it is the expected implementation for essentially all
installations. By adjusting the passed in `net.Listener` parameter, it should be
possible to include any needed additional support for load balancing or other
reverse proxying.

## Server Service Factories and Configuration

When messages arrive on a Fleetspeak server, they must be addressed to a
particular service - the `destination` field as described in
[Messages](#messages).  A Fleetspeak server runs one or more Services in order
to handle these messages.  By configuring additional independent services a
single Fleetspeak installation can handle messages for independent purposes.

A service is configured by a
[`fleetspeak.server.ServiceConfig`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/server/proto/fleetspeak_server/services.proto)
protocol buffer. Besides the name, used to address the service, the most important parameter
is `factory`. This string is used to look up a
[`service.Factory`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/service#Factory),
which determines what code to use to process incoming messages.

### Message Saver

As a particularly simple example, the fleetspeak demo directory includes a
[`messagesaver`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/demo/messagesaver/messagesaver.go)
package which defines a service which simply saves all received messages to text
files.

### GRPCService

The
[`grpcservice`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/grpcservice)
package provides a `service.Factory` which makes a [GRPC](https://grpc.io/) call
for each delivered message, in this way handing the message off to another
process.

It expects configuration parameters to be provided in a
[`fleetspeak.grpcservice.Config`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice/grpcservice.proto)
protocol buffer, and the target GRPC server must implement the
`fleetspeak.grpcservice.Processor` GRPC interface.

In addition to the factory, the grpcservice package also exports a [concrete
type](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/server/grpcservice#GRPCService)
which can be used to derive a GRPC-based service with more control over how and
when it dials a new GRPC target.


# Client

The Fleetspeak Client is a small process which runs on an endpoint and
communicates with a Fleetspeak Server.  Much like the server, it consists of a
base
[`client.Client`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client#Client)
along with a collection of components, and individual installations may adjust
these components according to specific needs.

## Miniclient: An Example

The
[`miniclient`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/demo/miniclient/miniclient.go)
binary from the demo directory defines a Fleetspeak Client in a general and
flexible way. Individual installations may want to define their own client
binary in order to hardcode security critical parameters (for example, before
signing it as a trusted binary) and otherwise adjust client behavior.

A Fleetspeak client must be provided with a list of TLS root certificates. The
client will then only communicate with a server if it presents one of these
certs, or a cert chaining to one of these certs. The `--trusted_cert_file` flag
is required by miniclient to set this list.

A Fleetspeek client must also be provided with the server (host and port) to
connect to. The client will require that the presented TLS server be valid for
this host. The `--server` flag is required by miniclient to set this server.

In addition, a Fleetspeak client needs a public key that it will use to check
the signature of externally provided service configurations. The implications of
this are discussed in more detail below. The `--deployment_key_file` flag is
required by miniclient to configure this.

To build a binary that can only be used by a specific Fleetspeak installation,
we recommend creating binary which hardcodes the configuration parameters set by
`--trusted_cert_file`, `--server`, and `--deployment_key_file`. Specifically, we
strongly recommend this if you will sign, whitelist a hash, or otherwise mark
the Fleetspeak client binary as trusted.

## Configuration Path

In addition to the security critical parameters described in the previous
section, a Fleetspeak client normally requires a configuration directory to
store its private key and to look for additional configuration. See the comments
on [`config.Configuration`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/config#Configuration) for details.

## Client Communicator

Every client requires a
[`Communicator`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/comms#Communicator)
component which handles communication with the server. This component defines how
the client communicates with the Fleetspeak server and should agree on protocol
with a corresponding [Server Communicator](#server-communicator).

The quintessential example is
[`https.Communicator`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/https#Communicator)
and at this point it is the expected implementation for essentially all
installations. By adjusting the `DialContext` attribute before starting the
Fleetspeak client, it should be possible to include any needed additional
support for specialized tunneling or proxying.

## Client Service Factories and Configuration

When messages arrive on a Fleetspeak client, they must be addressed to a
particular service - the `destination` field as described in
[Messages](#messages).  A Fleetspeak client runs one or more Services in order
to handle these messages, and create message to send to the server.  By
configuring additional independent services a single Fleetspeak client process
can handle messages for independent purposes.

A service is typically configured by dropping a
[`fleetspeak.SignedClientServiceConfig`](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/common/proto/fleetspeak/system.proto)
into `<config_path>/services/`. Besides the name, used to address the service,
the most important parameter is `factory`. This string is used to look up a
[`service.Factory`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/service#Factory),
which determines what code to use to process incoming messages.

This dropped configuration file must be signed with the deployment
key. Therefore by controlling access to the private part of the deployment key,
and hardcoding the public exponent into the client binary, allows an
installation to maintain strong control over what a particular client binary is
capable of.

### Stdinservice

The
[`stdinservice.Factory`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/stdinservice#Factory)
runs a binary with flags and standard input provided by a message, and returns
the output it produces. The service configuration determines what binary to
run. Every message received by the service causes and execution of the
binary. The configuration used in the [`demo`
directory](https://github.com/google/fleetspeak/tree/master/fleetspeak/src/demo)
sets up services based on this for the `ls` and `cat` binaries.

### Daemonservice

The
[`daemonservice.Factory`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/daemonservice#Factory)
handles the use case in which you want Fleetspeak to run a sub-process and
send/receive [messages](#messages) to/from it. This gives full control over what
is sent to the server, but requires more integration. The sub-process should use
the [`daemonservice` client
library](https://github.com/google/fleetspeak/tree/master/fleetspeak/src/client/daemonservice/client)
(currently available for go and python) to communicate through Fleetspeak.

### Socketservice

The
[`socketservice.Factory`](https://godoc.org/github.com/google/fleetspeak/fleetspeak/src/client/socketservice#Factory)
handles the use case in which you want Fleetspeak and some separately running
process to find each other and communicate using a local filesystem path,
e.g. through a UNIX domain socket.  Much like [`Daemonservice`](#daemonservice)
this also gives full control over what is sent to the server and requires some
integration. The sister process should use the [`socketservice` client
library](https://github.com/google/fleetspeak/tree/master/fleetspeak/src/client/socketservice/client)
to communicate through Fleetspeak.
