#!/bin/bash
openssl ecparam -list_curves

# generate a private key for a curve
openssl ecparam -name prime256v1 -genkey -noout -out key.pem

# optional: generate corresponding public key
openssl ec -in key.pem -pubout -out public-key.pem

# create a self-signed certificate
openssl req -new -x509 -key key.pem -out cert.pem -days 365 -subj "/C=AU/CN=fleetspeak-frontend" -addext "subjectAltName = DNS:fleetspeak-frontend"

FRONTEND_CERTIFICATE=$(sed ':a;N;$!ba;s/\n/\\\\n/g' cert.pem)
FRONTEND_KEY=$(sed ':a;N;$!ba;s/\n/\\\\n/g' key.pem)

sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./cleartext-header-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./cleartext-xfcc-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./direct-mtls-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./https-header-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./passthrough-mode/config/fleetspeak-client/config.textproto

sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./cleartext-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./cleartext-xfcc-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./direct-mtls-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./https-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' ./passthrough-mode/config/fleetspeak-server/components.textproto

sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' ./cleartext-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' ./cleartext-xfcc-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' ./direct-mtls-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' ./https-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' ./passthrough-mode/config/fleetspeak-server/components.textproto

cp cert.pem key.pem ./cleartext-header-mode/
cp cert.pem key.pem ./cleartext-xfcc-mode/
cp cert.pem key.pem ./direct-mtls-mode/
cp cert.pem key.pem ./https-header-mode/
cp cert.pem key.pem ./passthrough-mode/

MYSQL_PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9@%*+,-./' < /dev/urandom 2>/dev/null | head -c 16)
FLEETSPEAK_PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9@%*+,-./' < /dev/urandom 2>/dev/null | head -c 16)

sed -i 's@MYSQL_PASSWORD@'"$MYSQL_PASSWORD"'@' ./cleartext-header-mode/docker-compose.yaml
sed -i 's@MYSQL_PASSWORD@'"$MYSQL_PASSWORD"'@' ./cleartext-xfcc-mode/docker-compose.yaml
sed -i 's@MYSQL_PASSWORD@'"$MYSQL_PASSWORD"'@' ./direct-mtls-mode/docker-compose.yaml
sed -i 's@MYSQL_PASSWORD@'"$MYSQL_PASSWORD"'@' ./https-header-mode/docker-compose.yaml
sed -i 's@MYSQL_PASSWORD@'"$MYSQL_PASSWORD"'@' ./passthrough-mode/docker-compose.yaml

sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-header-mode/docker-compose.yaml
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-xfcc-mode/docker-compose.yaml
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./direct-mtls-mode/docker-compose.yaml
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./https-header-mode/docker-compose.yaml
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./passthrough-mode/docker-compose.yaml

sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-xfcc-mode/config/fleetspeak-server/components.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./direct-mtls-mode/config/fleetspeak-server/components.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./https-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./passthrough-mode/config/fleetspeak-server/components.textproto

sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-header-mode/config/fleetspeak.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./cleartext-xfcc-mode/config/fleetspeak.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./direct-mtls-mode/config/fleetspeak.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./https-header-mode/config/fleetspeak.textproto
sed -i 's@FLEETSPEAK_PASSWORD@'"$FLEETSPEAK_PASSWORD"'@' ./passthrough-mode/config/fleetspeak.textproto
