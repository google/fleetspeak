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
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"flag"
	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var adminAddr = flag.String("admin_addr", "localhost:6061", "Address for the admin server.")

func usage() {
	fmt.Fprint(os.Stderr,
		"Usage:\n"+
			"    admin_cli listclients\n"+
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
		log.Fatalf("Unable to connect to fleetspeak admin interface [%v]: %v", *adminAddr, err)
	}
	admin := sgrpc.NewAdminClient(conn)

	switch flag.Arg(0) {
	case "listclients":
		if flag.NArg() != 1 {
			fmt.Fprint(os.Stderr, "listclients takes no parameters.\n")
			usage()
			os.Exit(1)
		}
		listClients(admin)
	case "ls":
		startStdin(admin, "lsService", flag.Args()[1:]...)
	case "cat":
		startStdin(admin, "catService", flag.Args()[1:]...)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %v\n", flag.Arg(1))
		usage()
		os.Exit(1)
	}
}

func contactTime(c *spb.Client) time.Time {
	return time.Unix(c.LastContactTime.Seconds, int64(c.LastContactTime.Nanos))
}

// byContactTime adapts []*spb.Client for use by sort.Sort.
type byContactTime []*spb.Client

func (b byContactTime) Len() int           { return len(b) }
func (b byContactTime) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byContactTime) Less(i, j int) bool { return contactTime(b[i]).Before(contactTime(b[j])) }

func listClients(c sgrpc.AdminClient) {
	ctx := context.Background()
	res, err := c.ListClients(ctx, &spb.ListClientsRequest{})
	if err != nil {
		log.Fatalf("ListClients RPC failed: %v", err)
	}
	if len(res.Clients) == 0 {
		fmt.Println("No clients found.")
		return
	}
	sort.Sort(byContactTime(res.Clients))
	fmt.Printf("%-16s %-23s %s\n", "Client ID:", "Last Seen:", "Labels:")
	for _, cl := range res.Clients {
		id, err := common.BytesToClientID(cl.ClientId)
		if err != nil {
			log.Printf("Ignoring invalid client id [%v], %v", cl.ClientId, err)
			continue
		}
		var ls []string
		for _, l := range cl.Labels {
			ls = append(ls, l.ServiceName+":"+l.Label)
		}
		ts, err := ptypes.Timestamp(cl.LastContactTime)
		if err != nil {
			log.Printf("Unable to parse last contact time for %v: %v", id, err)
		}
		fmt.Printf("%v %v [%v]\n", id, ts.Format("15:04:05.000 2006.01.02"), strings.Join(ls, ","))
	}
}

func startStdin(c sgrpc.AdminClient, service string, args ...string) {
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}
	id, err := common.StringToClientID(args[0])
	if err != nil {
		log.Fatalf("Unable to parse %v as client id: %v", flag.Arg(1), err)
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
		log.Fatalf("Unable to marshal StdinServiceInputMessage as Any: %v", err)
	}
	_, err = c.InsertMessage(ctx, &m)
	if err != nil {
		log.Printf("InsertMessage RPC failed: %v", err)
	}
}
