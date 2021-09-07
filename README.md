# Fleetspeak

[![.github/workflows/build.yml](https://github.com/google/fleetspeak/actions/workflows/build.yml/badge.svg)](https://github.com/google/fleetspeak/actions/workflows/build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/google/fleetspeak)](https://goreportcard.com/report/github.com/google/fleetspeak)

Fleetspeak is a framework for communicating with a fleet of machines, with a
focus on security monitoring and basic administrative use cases.  It is a
subproject of [GRR](https://github.com/google/grr/blob/master/README.md), and
can be seen as an effort to modularizing and modernizing its communication
mechanism.

## Status

We have this code working internally as part of our GRR installation. Over the
next weeks to months we will be organizing our open source development process,
building out installation processes and otherwise preparing for broader external
use, both as part of GRR and for other uses.

## Getting Started
On linux, assuming a recent version of the go development environment (tested
with 1.9, probably requires at least 1.8) and virtualenv, the following sequence
of commands will build and test this pre-release:

```bash
go get -v -t github.com/google/fleetspeak/...

# Assuming default $GOPATH:
cd ~/go/src/github.com/google/fleetspeak

# Setup virtualenv - fleetspeak provides some python integration libraries,
# and this ensures they are set up in a known way.
virtualenv $HOME/FSENV
source $HOME/FSENV/bin/activate

# Pre-requisite to installing Fleetspeak (used to compile protos).
pip install grpcio-tools

pip install -e fleetspeak_python/

# Set mysql parameters. The mysql datastore test will run if the following environment
# variables are set. Otherwise it will be skipped.
export MYSQL_TEST_USER=<username>
export MYSQL_TEST_PASS=<password>   # will assume null password if unset.
export MYSQL_TEST_ADDR=<host:port>

# Build and test the release:
fleetspeak/build.sh
fleetspeak/test.sh
```

Once built, you can take a look at the files and instructions in our
[demo directory](https://github.com/google/fleetspeak/tree/master/fleetspeak/src/demo).

## DISCLAIMER

While the code presented here is in some sense feature complete, much of it is
barely tested or documented, and breaking changes are still possible.
Therefore, please consider this a preview release while the dust settles.
Suggestions and pull requests are very much appreciated.
