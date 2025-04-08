# Fleetspeak on Spanner

When operating Fleetspeak you need to decide on a persistence
[store](https://github.com/google/fleetspeak/blob/master/fleetspeak/src/server/db/store.go).

This document describes how to use [Spanner](https://cloud.google.com/spanner)
as the Fleetspeak datastore.

Spanner is a fully managed, mission-critical database service on
[Google Cloud](https://cloud.google.com) that brings together relational, graph,
key-value, and search. It offers transactional consistency at global scale,
automatic, synchronous replication for high availability.

## 1. Running Fleetspeak on Spanner

Running Fleetspeak on Spanner requires that you create and configure a
[Spanner instance](https://cloud.google.com/spanner/docs/instances) before you
run Fleetspeak.

Furthermore, you also need to create a
[Google Cloud Pub/Sub](https://cloud.google.com/pubsub)
[Topic](https://cloud.google.com/pubsub/docs/publish-message-overview) and
[Subscription](https://cloud.google.com/pubsub/docs/subscription-overview) that
are needed by the datastore to call the
[ProcessMessages()](https://github.com/daschwanden/fleetspeak/blob/a3f9826d0fb5419b47b5b4b0c50497723e35407b/fleetspeak/src/server/internal/services/manager.go#L181)
method on backlogged messages.

### 1.1. Google Cloud Spanner Instance

You can follow the instructions in the Google Cloud online documentation to
[create a Spanner instance](https://cloud.google.com/spanner/docs/create-query-database-console#create-instance).

> [!NOTE] You only need to create the
> [Spanner instance](https://cloud.google.com/spanner/docs/instances). The
> Fleetspeak [Spanner database](https://cloud.google.com/spanner/docs/databases)
> and its tables are created by running the provided [setup.sh](./setup.sh)
> script. The script assumes that you use `fleetspeak-instance` as the
> Fleetspeak instance name and `fleetspeak` as the Fleetspeak database name. In
> case you want to use different values then you need to update the
> [setup.sh](./setup.sh) script accordingly.

Run the following command to create the Fleetspeak database and its tables:

```bash
./setup.sh
```

### 1.2. Google Cloud Pub/Sub Topic and Subscription

You can follow the instructions in the Google Cloud online documentation to
[create a Pub/Sub Topic](https://cloud.google.com/pubsub/docs/create-topic#create_a_topic_2)
and to
[create a Pub/Sub Subscription](https://cloud.google.com/pubsub/docs/create-subscription#create_a_pull_subscription).

### 1.3. Fleetspeak Components Configuration

To run Fleetspeak on Spanner you need to configure the components settings with
the values of the Google Cloud Spanner and Pub/Sub resources created above.

The example below illustrates a sample Fleetspeak components configuration.

```bash
components_config {

  spanner_config {
    project_id: "YOUR_GOOGLE_CLOUD_PROJECT_ID_HERE"
    instance_name: "fleetspeak-instance"
    database_name: "fleetspeak"
    pubsub_topic: "YOUR_GOOGLE_CLOUD_PUBSUB_TOPIC_HERE"
    pubsub_subscription: "YOUR_GOOGLE_CLOUD_PUBSUB_SUBSCRIPTION_HERE"
  }

  https_config {
    ...
  }

  admin_config {
    ...
  }
}
```

## 2. Testing Fleetspeak on Spanner

For testing Fleetspeak on Spanner you can leverage the
[Spanner emulator](https://cloud.google.com/spanner/docs/emulator) and the
[PubSub emulator](https://cloud.google.com/pubsub/docs/emulator).

This avoids creating a real Spanner instance and PubSub Topic & Subscription so
you can run the tests without Google Cloud connectivity.

### 2.1. Docker Compose Test Setup

A convenient way to test Fleetspeak on Spanner is to leverage the
[Docker Compose](https://docs.docker.com/compose/) setup provided in this
[docker-compose.yaml](./docker-compose.yaml) file in this directory.

You can run the test suite by following the instructions below.

> [!NOTE] In case you choose different values for the Fleetspeak instance
> (default: `fleetspeak-instance`) and the Fleetspeak database (default:
> `fleetspeak`) then you will have to adjust these values in the
> [setup.sh](./setup.sh) script accordingly.

```bash
docker compose up -d

# Wait for all the services to be up then execute the commands below:
gcloud config configurations create emulator
gcloud config set auth/disable_credentials true
gcloud config set project spanner-emulator-project
gcloud config set api_endpoint_overrides/spanner http://localhost:9020/
gcloud config configurations activate emulator
gcloud spanner instances create fleetspeak-instance \
 --config=emulator-config --description="Fleetspeak Test Instance" --nodes=1

./setup.sh
```

In a seperate terminal run the followning commands:

```bash
docker exec -it spanner-setup-fleetspeak-1 bash
# You can now execute the following commands within the container:
export SPANNER_DATABASE=fleetspeak
export SPANNER_INSTANCE=fleetspeak-instance
export PUBSUB_EMULATOR_HOST=pubsub-emulator:8085
export PUBSUB_PROJECT_ID=spanner-emulator-project
export SPANNER_EMULATOR_HOST=spanner-emulator:9010
export SPANNER_PROJECT=fleetspeak-spanner
export SPANNER_SUBSCRIPTION=spanner-sub
export SPANNER_TOPIC=spanner-top

cd fleetspeak
source /venv/FSENV/bin/activate

go clean -testcache
./fleetspeak/test.sh
```
