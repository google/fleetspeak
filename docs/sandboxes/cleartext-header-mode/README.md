# Cleartext Header Mode

```
docker compose up --build -d

 ✔ Network cleartext-header-mode_default                Created                                                                                                      0.1s 
 ✔ Container cleartext-header-mode-front-envoy-1        Started                                                                                                      0.1s 
 ✔ Container cleartext-header-mode-mysql-server-1       Healthy                                                                                                      0.1s 
 ✔ Container cleartext-header-mode-fleetspeak-server-1  Healthy                                                                                                      0.0s 
 ✔ Container cleartext-header-mode-fleetspeak-client-1  Started                                                                                                      0.0s 
```

```
docker logs cleartext-header-mode-fleetspeak-client-1
I1121 09:36:44.932109       1 config.go:44] Read 1 trusted certificates.
E1121 09:36:44.933707       1 manager.go:103] initial load of writeback failed (continuing): open /fleetspeak-client.state: no such file or directory
I1121 09:36:44.934445       1 manager.go:165] Using new client id: 768dbfef556d2341
W1121 09:36:44.939751       1 client.go:175] No signed service configs could be read; continuing: invalid signed services directory path: unable to stat path [/config/fleetspeak-client/services]: stat /config/fleetspeak-client/services: no such file or directory
I1121 09:36:44.940864       1 services.go:146] Started service hello with config:
name:"hello"  factory:"Daemon"  config:{[type.googleapis.com/fleetspeak.daemonservice.Config]:{argv:"/venv/FSENV/bin/python"  argv:"/config/hello.py"}}
E1121 09:36:44.955173       1 system_service.go:251] Unable to get revoked certificate list: unable to retrieve file, last attempt failed with: failed with http response code: 404


docker run -it --name greeter --network cleartext-header-mode_default -p 1337:1337 --rm greeter bash


/venv/FSENV/bin/python ./greeter.py --client_id="YOUR_CLIENT_ID_HERE" --fleetspeak_message_listen_address="0.0.0.0:1337" --fleetspeak_server="fleetspeak-server:9091" --alsologtostderr

```


```
docker compose down
```
