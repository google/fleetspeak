#!/bin/bash
sudo su

while [[ ! -f frr_master_server_main ]]
do
	gsutil cp ${storage_bucket_url}/bin/frr_master_server_main ./
	sleep 10
done

chmod +x frr_master_server_main

./frr_master_server_main --listen_address=${master_server_host}:6059 --admin_address=${admin_host}:6061
