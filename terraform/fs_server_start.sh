#!/bin/bash

set -e

# apt_install command_to_check package_name
function apt_install {
    while [[ ! `command -v $1` ]];
    do
        apt-get -y update ||:
        apt-get -y install $2 ||:
        sleep 3
    done
}

apt_install pip3 python3-pip
apt_install mysql mysql-client

export PATH=/snap/bin:$PATH

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O ./cloud_sql_proxy
chmod +x ./cloud_sql_proxy

./cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &

ln -fs /usr/bin/python3 /usr/bin/python

#cp_from_bucket source_url destination
function cp_from_bucket {
    mkdir -p $(dirname $2)
    while [[ (! -f $2) && (! -e $2) ]]
    do
        gsutil cp -r $1 $(dirname $2) ||:
        sleep 5
    done
}

cp_from_bucket ${storage_bucket_url}/frr_python/wheel frr_python/wheel
pip3 install --target=frr_python frr_python/wheel/*
cp_from_bucket ${storage_bucket_url}/bin/server ./server
cp_from_bucket ${storage_bucket_url}/server_configs/server${self_index}.config ./server${self_index}.config
cp_from_bucket ${storage_bucket_url}/server_configs/server${self_index}.services.config ./server${self_index}.services.config
touch server${self_index}.ready
gsutil cp server${self_index}.ready ${storage_bucket_url}/started_components/

chmod +x server

./server -logtostderr -components_config "server${self_index}.config" -services_config "server${self_index}.services.config" &
python3 frr_python/frr_server.py --master_server_address=${master_server_host}:6059 --fleetspeak_message_listen_address=${self_host}:6062 --fleetspeak_server=${self_host}:6061
