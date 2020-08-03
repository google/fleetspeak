#!/bin/bash

set -e

while ! gsutil cp ${storage_bucket_url}/bin/frr_master_server_main ./; do
    sleep 10
done

chmod +x frr_master_server_main

./frr_master_server_main --listen_address=${master_server_host}:6059 --admin_address=${admin_host}:6061
