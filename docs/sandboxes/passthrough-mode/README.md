# Passthrough Mode

```
docker compose up --build -d

[+] Running 5/5
 ✔ Network passthrough-mode_default                            Created                                                                                               0.1s 
 ✔ Container passthrough-mode-mysql-server-1                   Healthy                                                                                               0.0s 
 ✔ Container passthrough-mode-fleetspeak-server-1              Started                                                                                               0.0s 
 ✔ Container passthrough-mode-fleetspeak-server-passthrough-1  Healthy                                                                                               0.0s 
 ✔ Container passthrough-mode-fleetspeak-client-1              Started                                                                                               0.0s
```

```
docker logs passthrough-mode-fleetspeak-client-1

I1121 11:06:00.704113       1 config.go:44] Read 1 trusted certificates.
E1121 11:06:00.704702       1 manager.go:103] initial load of writeback failed (continuing): open /fleetspeak-client.state: no such file or directory
I1121 11:06:00.705114       1 manager.go:165] Using new client id: 46cdb0f943ee8f44
W1121 11:06:00.706912       1 client.go:175] No signed service configs could be read; continuing: invalid signed services directory path: unable to stat path [/config/fleetspeak-client/services]: stat /config/fleetspeak-client/services: no such file or directory
I1121 11:06:00.710341       1 services.go:146] Started service hello with config:
name:"hello"  factory:"Daemon"  config:{[type.googleapis.com/fleetspeak.daemonservice.Config]:{argv:"/venv/FSENV/bin/python"  argv:"/config/hello.py"}}
E1121 11:06:00.723729       1 system_service.go:251] Unable to get revoked certificate list: unable to retrieve file, last attempt failed with: failed with http response code: 404

docker run -it --name greeter --network passthrough-mode_default -p 1337:1337 --rm greeter bash


/venv/FSENV/bin/python ./greeter.py --client_id="YOUR_CLIENT_ID_HERE" --fleetspeak_message_listen_address="0.0.0.0:1337" --fleetspeak_server="fleetspeak-server:9091" --alsologtostderr
```

```
docker compose down
```
