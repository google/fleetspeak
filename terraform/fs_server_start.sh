#!/bin/bash
sudo su

function waitlocks {
	while (fuser /var/lib/apt/lists/lock >/dev/null 2>&1) || (fuser /var/lib/dpkg/lock >/dev/null 2>&1) || (fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1); do
		sleep 3
	done
}

while [[ ! `command -v pip3` ]]; 
do
	waitlocks
	apt-get -y update
	waitlocks
	apt-get -y install python3-pip
done

waitlocks
apt-get -y install mysql-client

export PATH=/snap/bin:$PATH

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O ./cloud_sql_proxy
chmod +x ./cloud_sql_proxy

./cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &
sleep 3

ln -fs /usr/bin/python3 /usr/bin/python

pip3 install grpcio-tools absl-py fleetspeak

while [[ ! -f server ]]
do
	gsutil cp ${storage_bucket_url}/bin/server ./
	sleep 10
done

while [[ ! -f frr_server.py ]]
do
	gsutil cp ${storage_bucket_url}/frr_python/frr_server.py ./
	sleep 10
done

while [[ ! -f server${self_index}.config ]]
do
	gsutil cp ${storage_bucket_url}/server_configs/server${self_index}.config ./
	gsutil rm ${storage_bucket_url}/server_configs/server${self_index}.config 
	sleep 10
done

while [[ ! -f server${self_index}.services.config ]]
do
	gsutil cp ${storage_bucket_url}/server_configs/server${self_index}.services.config ./
	gsutil rm ${storage_bucket_url}/server_configs/server${self_index}.services.config 
	sleep 10
done

chmod +x server

./server -logtostderr -components_config "server${self_index}.config" -services_config "server${self_index}.services.config" &
python3 frr_server.py --master_server_address=${master_server_host}:6059 --fleetspeak_message_listen_address=${self_host}:6062 --fleetspeak_server=${self_host}:6061
