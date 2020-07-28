#!/bin/bash

# apt_install command_to_check package_name
function apt_install {
    while [[ ! `command -v $1` ]];
    do
        apt-get -y update
        apt-get -y install $2
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

#cp_from_bucket local_file_path_to_check source_url destination_directory
function cp_from_bucket {
    while [[ ! -f $1 ]]
    do
        gsutil cp $2 $3
        sleep 5
    done
}

mkdir frr_python_wheel
while [ ! "$(ls -A frr_python_wheel)" ]; do
    gsutil cp ${storage_bucket_url}/frr_python/wheel/* frr_python_wheel/
    sleep 5
done
pip3 install frr_python_wheel/*
cp_from_bucket server ${storage_bucket_url}/bin/server ./
cp_from_bucket frr_server.py ${storage_bucket_url}/frr_python/frr_server.py ./
cp_from_bucket server${self_index}.config ${storage_bucket_url}/server_configs/server${self_index}.config ./
gsutil rm ${storage_bucket_url}/server_configs/server${self_index}.config
cp_from_bucket server${self_index}.services.config ${storage_bucket_url}/server_configs/server${self_index}.services.config ./
gsutil rm ${storage_bucket_url}/server_configs/server${self_index}.services.config

chmod +x server

./server -logtostderr -components_config "server${self_index}.config" -services_config "server${self_index}.services.config" &
python3 frr_server.py --master_server_address=${master_server_host}:6059 --fleetspeak_message_listen_address=${self_host}:6062 --fleetspeak_server=${self_host}:6061
