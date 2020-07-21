#!/bin/bash
sudo su

mkdir /tmp/tmp
mkdir /tmp/tmp/go
export HOME=/tmp/tmp/
cd $HOME/go
snap install --classic --channel=1.14/stable go

sleep 1

export GOPATH=$HOME/go
export PATH=/snap/bin:$GOPATH/bin:$PATH

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
apt-get -y install python3-pip mysql-client

cd /tmp/tmp/go/src/github.com/Alexandr-TS/fleetspeak/
git checkout prep_cloud

ln -fs /usr/bin/python3 /usr/bin/python

pip3 install -e fleetspeak_python/
fleetspeak/build.sh

gsutil cp fleetspeak/src/e2etesting/frr-master-server-main/frr_master_server_main ${storage_bucket_url}/bin/frr_master_server_main
gsutil cp fleetspeak/src/client/client/client ${storage_bucket_url}/bin/client
gsutil cp fleetspeak/src/server/server/server ${storage_bucket_url}/bin/server
gsutil cp frr_python/frr_server.py ${storage_bucket_url}/frr_python/frr_server.py
gsutil cp frr_python/frr_client.py ${storage_bucket_url}/frr_python/frr_client.py

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O $HOME/cloud_sql_proxy
chmod +x $HOME/cloud_sql_proxy

~/cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &
sleep 3

pip3 install -e frr_python

mkdir terraform/tmp
echo "no results" > results.txt

echo ${admin_host} > server_hosts.txt

go run terraform/fleetspeak_configurator/build_configs.go --config_dir=terraform/tmp/ --num_clients=1 --num_servers=1 --mysql_address=127.0.0.1:3306 --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! < server_hosts.txt

gsutil cp terraform/tmp/server0.config ${storage_bucket_url}/configs/server0.config
gsutil cp terraform/tmp/server0.services.config ${storage_bucket_url}/configs/server0.services.config
gsutil cp terraform/tmp/linux_client0.config ${storage_bucket_url}/configs/linux_client0.config
gsutil cp terraform/tmp/textservices/frr.textproto ${storage_bucket_url}/protos/frr.textproto

while [[ `gsutil ls -r ${storage_bucket_url}/configs >/dev/null 2>&1; echo $?` == 0 ]]; do
	sleep 10
done

go run terraform/test_runner/run_tests.go --num_clients=1 --num_servers=1 --ms_address=${master_server_host}:6059 < server_hosts.txt > results.txt;
gsutil cp results.txt ${storage_bucket_url}
