#!/bin/bash

touch communicator.txt
touch client${self_index}.state
mkdir services

export PATH=/snap/bin:$PATH
ln -fs /usr/bin/python3 /usr/bin/python

# apt_install command_to_check package_name
function apt_install {
    while [[ ! `command -v $1` ]];
    do
        apt-get -y update
        apt-get -y install $2
        sleep 3
    done
}

#cp_from_bucket local_file_path_to_check source_url destination_directory
function cp_from_bucket {
    mkdir -p $(dirname $2)
    while [[ (! -f $2) && (! -e $2) ]]
    do
        gsutil cp $1 $(dirname $2)
        sleep 5
    done
}

apt_install pip3 python3-pip
cp_from_bucket ${storage_bucket_url}/frr_python/wheel/* frr_python/wheel/.
pip3 install frr_python/wheel/*
cp_from_bucket ${storage_bucket_url}/bin/client ./client
cp_from_bucket ${storage_bucket_url}/frr_python/frr_client.py frr_python/frr_client.py
cp_from_bucket ${storage_bucket_url}/protos/frr.textproto textservices/frr.textproto
cp_from_bucket ${storage_bucket_url}/client_configs/linux_client${self_index}.config linux_client${self_index}.config
touch client${self_index}.ready
gsutil cp client${self_index}.ready ${storage_bucket_url}/started_components/

chmod +x client

./client -logtostderr -config "linux_client${self_index}.config"
