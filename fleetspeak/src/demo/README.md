This directory contains programs, modules, and example configuration files to
run a small scale Fleetspeak server for demonstration purposes. It could also
serve as a starting point for a larger installation, but do read the
productionization notes below.

### Quick start:

Build all programs in this directory:

```bash
mkdir bin

go build -o bin/configurator configurator/configurator.go

go build -o bin/miniserver miniserver/miniserver.go

go build -o bin/miniclient miniclient/miniclient.go

go build -o bin/admin_cli admin_cli/admin_cli.go

go build -o bin/certgen certgen/certgen.go
```

Copy `configs/*` to `/tmp/fs_config/`

Run configurator with no (default) parameters, or equivalently:

```bash
bin/configurator --config_dir=/tmp/fs_config/ \
             --server_host_ports=localhost:6060
```

Then `/tmp/fs_config/` will contain the files you need to run a fleetspeak
client and server on the same machine.

To start the server:

```bash
bin/miniserver --admin_addr=localhost:6061 \
           --https_addr=localhost:6060 \
           --database_path=/tmp/fs_config/db.sqlite \
           --config_path=/tmp/fs_config/server_config.txt \
           --server_cert=/tmp/fs_config/server_cert.pem \
           --server_key=/tmp/fs_config/server_key.pem
```

To start the client:

```bash
bin/miniclient --config_path=/tmp/fs_config/client/ \
           --server=localhost:6060 \
           --trusted_cert_file=/tmp/fs_config/server_cert.pem \
           --deployment_key_file=/tmp/fs_config/deployment_key_public.pem
```

The demo client service configuration allows the client to run `/bin/ls` and
`/bin/cat` with arbitrary parameters. The demo server configuration recognizes
the responses from these operations and saves them in the directories
`/tmp/fs_saved/{ls,cat}Service/`. The `admin_cli` demo program accesses the
fleetspeak server through its administrative RPC interface in order to kick off
these operations.

Command to display the client ids that the server knows:

```bash
bin/admin_cli listclients
```

To list `/usr/bin` on client `9cd34bb2cdf87072`:

```bash
bin/admin_cli ls 9cd34bb2cdf87072 /usr/bin
```

To retrieve `/etc/passwd`:

```bash
bin/admin_cli cat 9cd34bb2cdf87072 /etc/passwd
```

The last two examples merely initiate the process. The results will appear after
some seconds in `/tmp/fs_saved/{ls,cat}Service/9cd34bb2cdf87072/`.

----

Productionization notes:

0) The `client_service_configs.txt` file describes the things that the client is
   permitted to do by the resulting configuration. The provided examples give
   the server a lot of power over the client and are meant for demo purposes
   only.

1) The `configurator` creates a server cert assuming that the client to connect
   to `localhost:6060`, or according to `server_host_ports` flag. You will need
   to recreate `server_cert.pem` if you change this - delete `server_cert.pem`
   to force recreation.

2) If a `server_cert.pem` is present, it will be used rather than created. This
   means that you may provide your own. In this case:

   - The `configurator` will not require or check `server_key.pem`, but of
     course the server will still need it.

   - Instead of providing the server certificate itself to the client, you may
     provide a CA root to the client.

   - The certificate provided by the server must be valid for the hostname that
     the client will attempt to connect to. (See note 1)

2.1) The `certgen` utility can be used to set up simple a CA & server
     configuration.  To use it, run it before `configurator`. See its
     introductory comments for more details.

3) It is expected that individual installations will branch `miniserver.go` in
   order to:

   - Add additional ServiceFactories, to enable different handling of received
     messages.

   - Retrieve keys and certs from non-file storage.

   - Use a more scalable datastore implementation than sqlite, e.g. the mysql
     datastore implementation.

   - Provide fine grained control of the administrative interface. By default
     anybody who can connect to its port can perform any administrative
     operation.

4) It is expected that individual installations will branch `miniclient.go` in
   order to:

   - Hardcode labels indicating the specific client version.

   - Hardcode the public exponent of deployment key, the trusted CA root list
     and the servers to connect to. Then if the binary is protected by signature
     or other hash identification, these critical parameters will fall under
     this protection.

   - Add additional ServiceFactories, to enable different types of client
     services.

5) The client requires a directory for service configurations, identity, and
   other state which should be stable across restarts. This demo uses
   `/tmp/fs_config/client` for this purpose, but in production it should be in a
   more stable location, e.g. `/etc/fleetspeak/`.

6) A new Fleetspeak service configuration can be distributed independently of
   the client.  For example, using `/etc/fleetspeak` for 5), a package
   integrating with Fleetspeak could write a new signed service configuration
   file to `/etc/fleetspeak/services/` on installation. Then on next restart,
   Fleetspeak would load the service.
