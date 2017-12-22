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

// Package cli contains methods useful for implementing administrative command
// line utilities.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// Usage prints usage information describing the command line flags and behavior
// of programs based on Execute.
func Usage() {
	n := path.Base(os.Args[0])
	fmt.Fprintf(os.Stderr,
		"Usage:\n"+
			"    %s listclients\n"+
			"\n", n)
}

// Execute examines command line flags and executes one of the standard command line
// actions, as summarized by Usage. It assumes that conn has a fleetspeak admin interface
// and that flag.Parse() has been called.
func Execute(conn *grpc.ClientConn) {
	admin := sgrpc.NewAdminClient(conn)

	if flag.NArg() == 0 {
		fmt.Fprint(os.Stderr, "A command is required.\n")
		Usage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "listclients":
		if flag.NArg() != 1 {
			fmt.Fprint(os.Stderr, "listclients takes no parameters.\n")
			Usage()
			os.Exit(1)
		}
		ListClients(admin)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %v\n", flag.Arg(0))
		Usage()
		os.Exit(1)
	}
}

// ListClients prints a list of all clients in the fleetspeak system.
func ListClients(c sgrpc.AdminClient) {
	ctx := context.Background()
	res, err := c.ListClients(ctx, &spb.ListClientsRequest{})
	if err != nil {
		log.Exitf("ListClients RPC failed: %v", err)
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
			log.Errorf("Ignoring invalid client id [%v], %v", cl.ClientId, err)
			continue
		}
		var ls []string
		for _, l := range cl.Labels {
			ls = append(ls, l.ServiceName+":"+l.Label)
		}
		ts, err := ptypes.Timestamp(cl.LastContactTime)
		if err != nil {
			log.Errorf("Unable to parse last contact time for %v: %v", id, err)
		}
		fmt.Printf("%v %v [%v]\n", id, ts.Format("15:04:05.000 2006.01.02"), strings.Join(ls, ","))
	}
}

// byContactTime adapts []*spb.Client for use by sort.Sort.
type byContactTime []*spb.Client

func (b byContactTime) Len() int           { return len(b) }
func (b byContactTime) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byContactTime) Less(i, j int) bool { return contactTime(b[i]).Before(contactTime(b[j])) }

func contactTime(c *spb.Client) time.Time {
	return time.Unix(c.LastContactTime.Seconds, int64(c.LastContactTime.Nanos))
}
