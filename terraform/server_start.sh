#!/bin/bash
sudo su

mkdir /tmp/tmp
mkdir /tmp/tmp/go
export HOME=/tmp/tmp/
cd $HOME/go
snap install --classic --channel=1.14/stable go

export GOROOT=/snap/go/current
export GOPATH=$HOME/go
export PATH=/snap/bin:$GOROOT/bin:$GOPATH/bin:$PATH

sleep 1
/snap/bin/go get -v -t github.com/Alexandr-TS/fleetspeak/...

function waitlocks {
	while (fuser /var/lib/dpkg/lock >/dev/null 2>&1) || (fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1); do
		sleep 3
	done
}

waitlocks
apt-get -y update
waitlocks
apt-get -y install python3-pip python3.6-venv mysql-client

cd /tmp/tmp/go/src/github.com/Alexandr-TS/fleetspeak/
git checkout prep_cloud

ln -fs /usr/bin/python3 /usr/bin/python

python3 -m venv /tmp/.venv/FLEETSPEAK
source /tmp/.venv/FLEETSPEAK/bin/activate

pip3 install -e fleetspeak_python/
fleetspeak/build.sh

gsutil cp fleetspeak/src/e2etesting/frr-master-server-main/frr_master_server_main ${storage_bucket_url}
gsutil cp fleetspeak/src/client/client/client ${storage_bucket_url}/bin/client
gsutil cp fleetspeak/src/server/server/server ${storage_bucket_url}/bin/server

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O $HOME/cloud_sql_proxy
chmod +x $HOME/cloud_sql_proxy

~/cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &
sleep 3

pip3 install -e frr_python

mkdir terraform/tmp
echo "no results" > results.txt
echo "localhost" > server_hosts.txt
go run terraform/main_vm.go --config_dir=terraform/tmp/ --num_clients=1 --num_servers=1 --mysql_address=127.0.0.1:3306 --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! --ms_address=${master_server_host}:6059 < server_hosts.txt > results.txt &
sleep 20

fleetspeak/src/server/server/server -logtostderr -components_config "terraform/tmp/server0.config" -services_config "terraform/tmp/server0.services.config" &
fleetspeak/src/client/client/client  -logtostderr -config "terraform/tmp/linux_client0.config" &
python frr_python/frr_server.py --master_server_address=${master_server_host}:6059 --fleetspeak_message_listen_address="localhost:6062" --fleetspeak_server="localhost:6060" &

sleep 40

gsutil cp results.txt ${storage_bucket_url}
