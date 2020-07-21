#!/bin/bash
sudo su

touch communicator.txt
touch client0.state
mkdir services
mkdir textservices
mkdir frr_python

export PATH=/snap/bin:$PATH
ln -fs /usr/bin/python3 /usr/bin/python


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

pip3 install grpcio-tools absl-py fleetspeak

while [[ ! -f client ]]
do
	gsutil cp ${storage_bucket_url}/bin/client ./
	sleep 10
done

while [[ ! -f frr_python/frr_client.py ]]
do
	gsutil cp ${storage_bucket_url}/frr_python/frr_client.py frr_python
	sleep 10
done

while [[ ! -f linux_client0.config ]]
do
	gsutil cp ${storage_bucket_url}/configs/linux_client0.config ./
	gsutil rm ${storage_bucket_url}/configs/linux_client0.config 
	sleep 10
done

while [[ ! -f textservices/frr.textproto ]]
do
	gsutil cp ${storage_bucket_url}/protos/frr.textproto textservices/
	sleep 10
done

chmod +x client 

./client -logtostderr -config "linux_client0.config"
