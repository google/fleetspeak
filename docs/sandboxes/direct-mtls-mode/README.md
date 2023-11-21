# Direct mTLS Mode

```
docker compose up --build -d

[+] Running 4/4
 ✔ Network direct-mtls-mode_default                Created                                                                                                           0.1s 
 ✔ Container direct-mtls-mode-mysql-server-1       Healthy                                                                                                           0.0s 
 ✔ Container direct-mtls-mode-fleetspeak-server-1  Healthy                                                                                                           0.0s 
 ✔ Container direct-mtls-mode-fleetspeak-client-1  Started                                                                                                           0.0s 
```

```
docker logs direct-mtls-mode-fleetspeak-client-1
I1121 10:10:37.346727       1 config.go:44] Read 1 trusted certificates.
E1121 10:10:37.348422       1 manager.go:103] initial load of writeback failed (continuing): open /fleetspeak-client.state: no such file or directory
I1121 10:10:37.349220       1 manager.go:165] Using new client id: c9645b21aa7edc47
W1121 10:10:37.353287       1 client.go:175] No signed service configs could be read; continuing: invalid signed services directory path: unable to stat path [/config/fleetspeak-client/services]: stat /config/fleetspeak-client/services: no such file or directory
I1121 10:10:37.354211       1 services.go:146] Started service hello with config:
name:"hello"  factory:"Daemon"  config:{[type.googleapis.com/fleetspeak.daemonservice.Config]:{argv:"/venv/FSENV/bin/python"  argv:"/config/hello.py"}}
E1121 10:10:37.365635       1 system_service.go:251] Unable to get revoked certificate list: unable to retrieve file, last attempt failed with: failed with http response code: 404


docker run -it --name greeter --network direct-mtls-mode_default -p 1337:1337 --rm greeter bash


/venv/FSENV/bin/python ./greeter.py --client_id="YOUR_CLIENT_ID_HERE" --fleetspeak_message_listen_address="0.0.0.0:1337" --fleetspeak_server="front-envoy:9091" --alsologtostderr
```

```
docker compose down
```
