#!/bin/bash

TIME_LIMIT=1800

# apt_install command_to_check package_name
function apt_install {
    while [[ ! `command -v $1` ]];
    do
        apt-get -y update
        apt-get -y install $2
        sleep 3
    done
}

function log {
    echo $(date -u) - $* >> $HOME/results.txt
}

function retry {
    while ! eval $*; do
        sleep 10
        if [[ $SECONDS -gt $TIME_LIMIT ]]; then
            return 1
        fi
    done
    return 0
}

mkdir go
export HOME=/
cd $HOME/go
log "Script started"
SECONDS=0

apt_install pip3 python3-pip
apt_install mysql mysql-client

snap install --classic --channel=1.14/stable go

export GOPATH=$HOME/go
export PATH=/snap/bin:$GOPATH/bin:$PATH

/snap/bin/go get -v -t github.com/Alexandr-TS/fleetspeak/...

cd $HOME/go/src/github.com/Alexandr-TS/fleetspeak/
git checkout tmp_prep_cloud

ln -fs /usr/bin/python3 /usr/bin/python

pip3 install --user --upgrade setuptools wheel
python3 frr_python/setup.py bdist_wheel

gsutil cp frr_python/dist/* ${storage_bucket_url}/frr_python/wheel/
pip3 install fleetspeak
fleetspeak/build.sh
gsutil cp fleetspeak/src/e2etesting/frr-master-server-main/frr_master_server_main ${storage_bucket_url}/bin/frr_master_server_main
gsutil cp fleetspeak/src/client/client/client ${storage_bucket_url}/bin/client
gsutil cp fleetspeak/src/server/server/server ${storage_bucket_url}/bin/server
gsutil cp frr_python/frr_server.py ${storage_bucket_url}/frr_python/frr_server.py
gsutil cp frr_python/frr_client.py ${storage_bucket_url}/frr_python/frr_client.py

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O $HOME/cloud_sql_proxy
chmod +x $HOME/cloud_sql_proxy
$HOME/cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &

mkdir terraform/tmp

seq -f ${ip_fs_server_prefix}.%g 0 $((${num_servers}-1)) > server_hosts.txt;

go run terraform/fleetspeak_configurator/build_configs.go --config_dir=terraform/tmp/ --num_clients=${num_clients} --num_servers=${num_servers} --mysql_address=127.0.0.1:3306 --mysql_database=fleetspeak_test --mysql_username=fsuser --mysql_password=fsuserPass1! < server_hosts.txt

for i in $(seq 0 $((${num_servers}-1))); do
    gsutil cp terraform/tmp/server$${i}.config ${storage_bucket_url}/server_configs/server$${i}.config
    gsutil cp terraform/tmp/server$${i}.services.config ${storage_bucket_url}/server_configs/server$${i}.services.config
done

if retry '[ $(gsutil ls ${storage_bucket_url}/started_components/server*ready | wc -l) -eq ${num_servers} ]'; then
    log "All servers connected"
else
    log "Not all servers connected within 30 minutes. Probably some of the servers failed to start, and the error occured before starting Fleetspeak. Try to check servers logs and restart the test."
fi

sleep 30

gsutil cp terraform/tmp/textservices/frr.textproto ${storage_bucket_url}/protos/frr.textproto

for i in $(seq 0 $((${num_clients}-1))); do
    gsutil cp terraform/tmp/linux_client$${i}.config ${storage_bucket_url}/client_configs/linux_client$${i}.config
done

if retry '[ $(gsutil ls -r ${storage_bucket_url}/started_components/client*ready | wc -l) -eq ${num_clients} ]'; then
    log "All clients connected"
else
    log "Not all clients connected within 30 minutes. Probably some of the clients failed to start, and the error occured before starting Fleetspeak. Try to check clients logs and restart the test."
fi

go run terraform/test_runner/run_tests.go --num_clients=${num_clients} --servers_file=server_hosts.txt --ms_address=${master_server_host}:6059 >> $HOME/results.txt
log "Script finished"
gsutil cp $HOME/results.txt ${storage_bucket_url}
