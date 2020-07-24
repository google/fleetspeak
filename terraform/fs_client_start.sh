#!/bin/bash

touch communicator.txt
touch client${self_index}.state
mkdir services
mkdir textservices
mkdir frr_python

export PATH=/snap/bin:$PATH
ln -fs /usr/bin/python3 /usr/bin/python

while [[ ! `command -v pip3` ]];
do
    apt-get -y update
    apt-get -y install python3-pip
done

pip3 install grpcio-tools fleetspeak

#cp_from_bucket local_file_path_to_check source_url destination_directory
function cp_from_bucket {
    while [[ ! -f $1 ]]
    do
        gsutil cp $2 $3
        sleep 5
    done
}

cp_from_bucket client ${storage_bucket_url}/bin/client ./
cp_from_bucket frr_python/frr_client.py ${storage_bucket_url}/frr_python/frr_client.py frr_python/
cp_from_bucket textservices/frr.textproto ${storage_bucket_url}/protos/frr.textproto textservices/
cp_from_bucket linux_client${self_index}.config ${storage_bucket_url}/client_configs/linux_client${self_index}.config ./
gsutil rm ${storage_bucket_url}/client_configs/linux_client${self_index}.config

chmod +x client

./client -logtostderr -config "linux_client${self_index}.config"
