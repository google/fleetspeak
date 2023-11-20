# Frontend mode Sandboxes

We have created a number of sandboxes using Docker Compose that set up environments to test out Fleetspeak’s frontend mode features and show sample configurations.

These can be used to learn about Fleetspeak's frontend mode options and how to model your own configurations.
The sandboxes use a containerised version of the Fleetspeak demo setup described in the [guide documentation page](https://github.com/google/fleetspeak/blob/master/docs/guide.md). 

Before you begin you will need to install the sandbox environment.

## Setup the sandbox environment
- [Install Docker](#install-docker)
- [Install docker compose](#install-docker-compose)
- [Install Git](#install-git)
- [Clone the Fleetspeak repository](#clone-the-fleetspeak-repository)

## The following sandboxes are available
- [Direct mTLS mode](./sandboxes/direct-mtls-mode)
    - end-to-end mTLS
    - Fleetspeak's original design
- [Passthrough mode](./sandboxes/passthrough-mode)
    - TCP proxy passthrough
- [HTTPS header mode](./sandboxes/https-header-mode)
    - L7 proxy terminates mTLS connection
    - Proxy passes client side certificate and checksum via HTTP headers
    - TLS connection from proxy to Fleetspeak
- [Cleartext header mode(./sandboxes/cleartext-header-mode)
     - L7 proxy terminates mTLS connection
    - Proxy passes client side certificate and checksum via HTTP headers
    - Cleartext connection from proxy to Fleetspeak

## Setup instructions

### Install docker
Ensure that you have a recent versions of ```docker``` installed.

You will need a minimum version of ```19.03.0+```.

Version ```20.10``` is well tested, and has the benefit of included ```compose```.

The user account running the examples will need to have permission to use Docker on your system.

Full instructions for installing Docker can be found on the [Docker website](https://docs.docker.com/get-docker/).  


### Install docker compose
The examples use [Docker compose configuration version 3.8](https://docs.docker.com/compose/compose-file/compose-versioning/#version-38).

You will need to a fairly recent version of [Docker Compose](https://docs.docker.com/compose/)  

### Install Git
The Fleetspeak project repository is managed using [Git](https://git-scm.com/).

You can [find instructions for installing Git on various operating systems here](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git).  


### Clone the Fleetspeak repository
If you have not cloned the Fleetspeak repository already, clone it with:

```
git clone https://github.com/google/fleetspeak
```
