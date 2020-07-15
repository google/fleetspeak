#!/bin/bash
sudo su
mkdir /tmp/tmp
mkdir /tmp/tmp/go
export HOME=/tmp/tmp/
cd $HOME/go
snap install --classic --channel=1.14/stable go

export GOROOT=/snap/go/current
export GOPATH=$HOME/go
export PATH=/snap/bin:$GOROOT/bin:$GOPATH/bin:$PATH

/snap/bin/go get -v -t github.com/Alexandr-TS/fleetspeak/...

function waitlocks {
	while (fuser /var/lib/dpkg/lock >/dev/null 2>&1) || (fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1); do
		sleep 3
	done
}

waitlocks
apt-get -y update
waitlocks
apt-get -y install python3-pip python3.6-venv mysql-client

cd /tmp/tmp/go/src/github.com/Alexandr-TS/fleetspeak/
git checkout prep_cloud

ln -fs /usr/bin/python3 /usr/bin/python

python3 -m venv /tmp/.venv/FLEETSPEAK
source /tmp/.venv/FLEETSPEAK/bin/activate

pip3 install -e fleetspeak_python/
fleetspeak/build.sh

wget https://dl.google.com/cloudsql/cloud_sql_proxy.linux.amd64 -O $HOME/cloud_sql_proxy
chmod +x $HOME/cloud_sql_proxy

~/cloud_sql_proxy -instances=${mysql_instance_connection_name}=tcp:3306 &
sleep 2

fleetspeak/src/e2etesting/e2etest.sh
