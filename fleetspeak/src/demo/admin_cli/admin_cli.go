// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/admin/cli"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var adminAddr = flag.String("admin_addr", "localhost:6061", "Address for the admin server.")

func usage() {
	fmt.Fprint(os.Stderr,
		"Usage:\n"+
			"    admin_cli listclients\n"+
			"    admin_cli listcontacts <client_id> [limit]\n"+
			"    admin_cli ls <client_id> path...\n"+
			"    admin_cli cat <client_id> path...\n"+
			"\n"+
			"    --admin_addr The address of the fleetspeak admin interface.\n")
}

func main() {
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprint(os.Stderr, "A command is required.\n")
		usage()
		os.Exit(1)
	}

	conn, err := grpc.Dial(*adminAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Exitf("Unable to connect to fleetspeak admin interface [%v]: %v", *adminAddr, err)
	}
	admin := sgrpc.NewAdminClient(conn)

	switch flag.Arg(0) {
	case "listclients":
		cli.ListClients(admin, flag.Args()[1:]...)
	case "listcontacts":
		cli.ListContacts(admin, flag.Args()[1:]...)
	case "ls":
		startStdin(admin, "lsService", flag.Args()[1:]...)
	case "cat":
		startStdin(admin, "catService", flag.Args()[1:]...)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %v\n", flag.Arg(0))
		usage()
		os.Exit(1)
	}
}

func startStdin(c sgrpc.AdminClient, service string, args ...string) {
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}
	id, err := common.StringToClientID(args[0])
	if err != nil {
		log.Exitf("Unable to parse %v as client id: %v", flag.Arg(1), err)
	}

	ctx := context.Background()
	im := sspb.InputMessage{Args: args[1:]}
	m := fspb.Message{
		Source:      &fspb.Address{ServiceName: service},
		Destination: &fspb.Address{ClientId: id.Bytes(), ServiceName: service},
		MessageType: "StdinServiceInputMessage",
	}
	m.Data, err = ptypes.MarshalAny(&im)
	if err != nil {
		log.Exitf("Unable to marshal StdinServiceInputMessage as Any: %v", err)
	}
	_, err = c.InsertMessage(ctx, &m)
	if err != nil {
		log.Errorf("InsertMessage RPC failed: %v", err)
	}
}
