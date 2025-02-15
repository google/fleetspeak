#!/bin/bash
export ACTIONS=8

echo "1/$ACTIONS : installing the required tooling..."
apt update
apt install -y git python3.11-venv pip

echo "2/$ACTIONS : starting venv..."
python3 -m venv /venv
source /venv/bin/activate

echo "3/$ACTIONS : clone the python pubsub repo..."
git clone https://github.com/googleapis/python-pubsub.git

echo "4/ installing dependencies..."
cd python-pubsub/samples/snippets
pip install -r requirements.txt

echo "5/$ACTIONS : exporting env vars..."
export PUBSUB_EMULATOR_HOST=localhost:8085
export PUBSUB_PROJECT_ID=some-project-id

echo "6/$ACTIONS : exporting env vars..."
gcloud beta emulators pubsub start --project=some-project-id --host-port='0.0.0.0:8085' &

echo "7/$ACTIONS : creating the topic..."
python3 publisher.py some-project-id create some-topic

echo "8/$ACTIONS : creating the subscription..."
python3 subscriber.py some-project-id create some-topic some-subscription