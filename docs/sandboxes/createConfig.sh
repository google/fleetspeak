#!/bin/bash
openssl ecparam -list_curves

# generate a private key for a curve
openssl ecparam -name prime256v1 -genkey -noout -out key.pem

# optional: generate corresponding public key
openssl ec -in key.pem -pubout -out public-key.pem

# create a self-signed certificate
openssl req -new -x509 -key key.pem -out cert.pem -days 365 -subj "/C=AU/CN=front-envoy" -addext "subjectAltName = DNS:front-envoy"

FRONTEND_CERTIFICATE=$(sed ':a;N;$!ba;s/\n/\\\\n/g' cert.pem)
FRONTEND_KEY=$(sed ':a;N;$!ba;s/\n/\\\\n/g' key.pem)

sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/cleartext-header-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/direct-mtls-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/https-header-mode/config/fleetspeak-client/config.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/passthrough-mode/config/fleetspeak-client/config.textproto

sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/cleartext-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/direct-mtls-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/https-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_CERTIFICATE@'"$FRONTEND_CERTIFICATE"'@' sandboxes/passthrough-mode/config/fleetspeak-server/components.textproto

sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' sandboxes/cleartext-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' sandboxes/direct-mtls-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' sandboxes/https-header-mode/config/fleetspeak-server/components.textproto
sed -i 's@FRONTEND_KEY@'"$FRONTEND_KEY"'@' sandboxes/passthrough-mode/config/fleetspeak-server/components.textproto

cp cert.pem key.pem sandboxes/cleartext-header-mode/
cp cert.pem key.pem sandboxes/direct-mtls-mode/
cp cert.pem key.pem sandboxes/https-header-mode/
cp cert.pem key.pem sandboxes/passthrough-mode/
