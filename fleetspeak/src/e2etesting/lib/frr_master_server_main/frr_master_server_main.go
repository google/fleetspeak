package main

import (
	"flag"
	"fmt"
	frr "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	"google.golang.org/grpc"
	"net"
	"os"
)

var (
	listenAddr = flag.String("listen_address", "localhost:6059", "Address for clients to connect")
)

// StartMasterServer starts FRR Master Server listening to listenAddr
func StartMasterServer(listenAddr string) error {
	ms := frr.NewMasterServer(nil)
	gms := grpc.NewServer()
	fgrpc.RegisterMasterServer(gms, ms)
	ad, err := net.ResolveTCPAddr("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("Unable to resolve tcp address: %v", err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		return fmt.Errorf("Unable to start listening TCP: %v", err)
	}
	defer gms.Stop()
	gms.Serve(tl)
	return nil
}

func main() {
	flag.Parse()
	err := StartMasterServer(*listenAddr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
