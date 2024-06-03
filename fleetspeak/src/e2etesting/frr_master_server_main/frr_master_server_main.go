package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	frr "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	listenAddr = flag.String("listen_address", "localhost:6059", "Address for clients to connect")
	adminAddr  = flag.String("admin_address", "localhost:6061", "Fleetspeak server admin address")
)

// StartMasterServer starts FRR Master Server listening to listenAddr
func StartMasterServer(listenAddr, adminAddr string) error {
	conn, err := grpc.NewClient(adminAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("Unable to connect to FS server: %v", err)
	}
	defer conn.Close()

	ms := frr.NewMasterServer(sgrpc.NewAdminClient(conn))
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
	err := StartMasterServer(*listenAddr, *adminAddr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
