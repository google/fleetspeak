Guide
=====

This guide will walk you through setting up your own Fleetspeak instance (both
the client and the server) by compiling everything yourself and explaining all
the concepts along the way. For a more detailed description of all components,
please refer to the [design][design] document.

By the end of this guide, you should have a working Fleetspeak instance with a
simple server-side service that is able to communicate with a client-side
service and a general idea how to extend this setup to more sophisticated tasks.

[design]: ../DESIGN.md


MySQL setup
-----------

Fleetspeak uses MySQL as its primary data store. Make sure that you have a
relatively new MySQL or MariaDB installation—please refer to the official
documentation for instructions specific to your platform.

Fire-up the MySQL console as an administrative user, e.g.:

    $ mysql --user root

Create a Fleetspeak database and an associated database user:

    mysql> CREATE USER `fleetspeak-user` IDENTIFIED BY 'fleetspeak-password';
    mysql> CREATE DATABASE `fleetspeak`;
    mysql> GRANT ALL PRIVILEGES ON `fleetspeak`.* TO `fleetspeak-user`;

Remember the database name, the username and the password—they will be specified
later in the Fleetspeak configuration file.


Fleetspeak compilation
----------------------

Fleetspeak is written in the [Go programming language][golang]. Therefore, in
order to compile it you need to install the Go development environment on your
system. As always, refer to the official documentation on how to do it (or use
your platform's package manager).

In order to simply compile everything, setup `$GOPATH` to a folder of your
choice (it will be used by the Go to store the sources and resulting binary
files) and run the `go get` tool:

    $ export GOPATH="$HOME/fleetspeak"
    $ go get github.com/google/fleetspeak/...

That's it! Now, in your `$GOPATH/bin` you should have multiple Fleetspeak binary
files available: `admin`, `client`, `config`, `server`.


[golang]: https://golang.org/


Fleetspeak configuration
------------------------

Fleetspeak uses a bunch of configuration files written in the
[Protocol Buffers][protobuf] text format. Thanks to this, you can always inspect
the proto definition to learn about the schema when in doubt.

We will start by creating a basic configuration file for the Fleetspeak
configurator—a binary that generates more specific configuration files,
certificates and keys.

Note that the configurator is currently not able to expand variables. Because of
this, all the paths provided in the configuration files need to be absolute and
already fully expanded. In all the examples below we will assume that your
`$HOME` expands to `/home/user`, so make sure to adjust it accordingly.

Save the following in some file, e.g. `$HOME/.config/fleetspeak.textproto`:

```
configuration_name: "Example"

components_config {

  mysql_data_source_name: "fleetspeak-user:fleetspeak-password@/fleetspeak"

  https_config {
    listen_address: "localhost:9090"
  }

  admin_config {
    listen_address: "localhost:9091"
  }
}

public_host_port: "localhost:9090"

trusted_cert_file: "/home/user/.config/fleetspeak-server/ca.pem"
trusted_cert_key_file: "/home/user/.config/fleetspeak-server/ca-key.pem"

server_cert_file: "/home/user/.config/fleetspeak-server/server.pem"
server_cert_key_file: "/home/user/.config/fleetspeak-server/server-key.pem"

server_component_configuration_file: "/home/user/.config/fleetspeak-server/components.textproto"
linux_client_configuration_file: "/home/user/.config/fleetspeak-client/config.textproto"
```

Refer to the schema for details about each of these preferences and other
possible options.

Create the directories if needed and run the configurator:

    $ mkdir $HOME/.config/fleetspeak-server $HOME/.config/fleetspeak-client
    $ $GOPATH/bin/config -config "$HOME/.config/fleetspeak.textproto"

This should generate a bunch of configuration files:

  * `$HOME/.config/fleetspeak-server/ca.pem`
  * `$HOME/.config/fleetspeak-server/ca-key.pem`
  * `$HOME/.config/fleetspeak-server/server.pem`
  * `$HOME/.config/fleetspeak-server/server-key.pem`
  * `$HOME/.config/fleetspeak-server/components.textproto`
  * `$HOME/.config/fleetspeak-client/config.textproto`

[protobuf]: https://developers.google.com/protocol-buffers/

### Server configuration

For now, since we do not want to create any services yet, create an empty
services configuration file:

    $ touch $HOME/.config/fleetspeak-server/services.textproto

It should be possible to run the server now:

    $ $GOPATH/bin/server \
        -components_config "$HOME/.config/fleetspeak-server/components.textproto" \
        -services_config "$HOME/.config/fleetspeak-server/services.textproto" \
        -alsologtostderr

In the terminal, you should see a log message that the Fleetspeak server has
started.

### Client configuration

In the generated `$HOME/.config/fleetspeak-client/config.textproto` adjust paths
of the filesystem handler, e.g.:

```
(...)
filesystem_handler: <
  configuration_directory: "/home/user/.config/fleetspeak-client/"
  state_file: "/home/user/.local/share/fleetspeak.state"
>
(...)
```

Create folders where the future client services configuration will be stored and
an (empty) communicator configuration. These files are not strictly necessary,
but we create them to avoid confusing warnings.

    $ mkdir $HOME/.config/fleetspeak-client/services
    $ mkdir $HOME/.config/fleetspeak-client/textservices
    $ touch $HOME/.config/fleetspeak-client/communicator.txt

It should be possible to run the client now:

    $ $GOPATH/bin/client \
        -config "$HOME/.config/fleetspeak-client/config.textproto" \
        -alsologtostderr

It the terminal you should see a log message with see the client id (remember it
for later) and, if the server is running, the client should not complain about
the connection being refused.


Fleetspeak services
-------------------

Fleetspeak is just a message delivery system and its pretty useless on its own.
In order to do something useful with it, we need services that want to talk with
each other.

### Client services

We will write a simple client service that accepts messages with a name and
responds with a greeting. That service will be written in [Python][python] using
a [Fleetspeak connector package][python-fleetspeak]. For this you will need a
Python 3 environment installed on your system, including the `pip` package
manager.

In this guide we will use [virtualenv][virtualenv] to avoid cluttering the
system namespace, but you can install everything globally, if you prefer.
Initialize the virtual environment and install necessary packages:

    $ python3 -m venv $HOME/.venv/FLEETSPEAK
    $ $HOME/.venv/FLEETSPEAK/bin/pip install absl-py protobuf fleetspeak

[python]: https://python.org/
[python-fleetspeak]: https://pypi.org/project/fleetspeak/
[virtualenv]: https://virtualenv.pypa.io/

Create the following Python script and save it somewhere, e.g. `$HOME/hello.py`:

```python
from absl import app
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from fleetspeak.client_connector.connector import FleetspeakConnection
from google.protobuf.wrappers_pb2 import StringValue


def main(argv):
    del argv  # Unused.

    conn = FleetspeakConnection(version="0.0.1")
    while True:
        request, _ = conn.Recv()

        data = StringValue()
        request.data.Unpack(data)

        data.value = f"Hello {data.value}!"

        response = Message()
        response.destination.service_name = request.source.service_name
        response.data.Pack(data)

        conn.Send(response)


if __name__ == "__main__":
    app.run(main)
```

The program simply creates a connection to the Fleetspeak client and loops
waiting for messages indefinitely. In this example, to avoid writing boilerplate
involved with creating custom Protocol Buffer message definitions, we simply
use the `StringValue` message—a standard wrapper around the `string` type.

Because this program makes sense only within the virtual environment, we also
need a wrapper script that runs it. Save the following somewhere, e.g.
`$HOME/hello.sh`:

```
#!/usr/bin/env bash
$HOME/.venv/FLEETSPEAK/bin/python "$HOME/hello.py"
```

This script should be an executable:

    $ chmod +x $HOME/hello.sh

Finally, we need to make the Fleetspeak client aware of this service. For this
purpose, create the following `hello.service` and place it the `textservices`
folder of the Fleetspeak client configuration:

```
name: "hello"
factory: "Daemon"
config: {
  [type.googleapis.com/fleetspeak.daemonservice.Config]: {
    argv: "$HOME/hello.sh"
  }
}
```

Once again, note that Fleetspeak does not understand shell variables, so you
need to expand `$HOME` manually it the script above.

### Server services

We will create a server-side service `greeter` that will read a name from the
user, send it to the specified client and print the response.

Save the following script into some file, e.g. `$HOME/greeter.py`:

```python
import binascii
import logging

from absl import app
from absl import flags
from fleetspeak.server_connector.connector import InsecureGRPCServiceClient
from fleetspeak.src.common.proto.fleetspeak.common_pb2 import Message
from google.protobuf.wrappers_pb2 import StringValue


FLAGS = flags.FLAGS

flags.DEFINE_string(
    name="client_id",
    default="",
    help="An id of the client to send the messages to.")


def listener(message, context):
    del context  # Unused

    data = StringValue()
    message.data.Unpack(data)
    logging.info(f"RESPONSE: {data.value}")


def main(argv=None):
    del argv  # Unused.

    service_client = InsecureGRPCServiceClient("greeter")
    service_client.Listen(listener)

    while True:
        data = StringValue()
        data.value = input("Enter your name: ")

        request = Message()
        request.destination.client_id = binascii.unhexlify(FLAGS.client_id)
        request.destination.service_name = "hello"
        request.data.Pack(data)

        service_client.Send(request)


if __name__ == "__main__":
    app.run(main)
```

In an infinite loop, we read the user input with the `input` function and send
it to the `hello` service on the client specified in a flag. The client response
will eventually get delivered to the `listener` function and printed.

Now, we must make the server aware of the service. Pick some unused port that
the service is going to run on, e.g. 1337, and put the following to the
`services.textproto` of the Fleetspeak server configuration:

```
services {
  name: "greeter"
  factory: "GRPC"
  config: {
    [type.googleapis.com/fleetspeak.grpcservice.Config] {
      target: "localhost:1337"
      insecure: true
    }
  }
}
```

Running
-------

Everything should now be properly set-up and ready to run. First, launch the
server:

    $ $GOPATH/bin/server \
        -components_config "$HOME/.config/fleetspeak-server/components.textproto" \
        -services_config "$HOME/.config/fleetspeak-server/services.textproto" \
        -alsologtostderr

In a separate terminal, launch the client and note down the client identifier:

    $ $GOPATH/bin/client \
        -config "$HOME/.config/fleetspeak-client/config.textproto" \
        -alsologtostderr

Finally, in yet another terminal, run the server service with the appropriate
client identifier:

    $ HOME/.venv/FLEETSPEAK/bin/python $HOME/greeter.py \
        --client_id="d741e09e257bf4ba" \
        --fleetspeak_message_listen_address="localhost:1337" \
        --fleetspeak_server="localhost:9091" \
        --alsologtostderr

Enter your name as requested and soon a message with response should be logged.
