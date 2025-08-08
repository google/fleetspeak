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
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/admin/history"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// dateFmt is a fairly dense, 23 character date format string, suitable for
// tabular date information.
const dateFmt = "15:04:05.000 2006.01.02"

// Usage prints usage information describing the command line flags and behavior
// of programs based on Execute.
func Usage() {
	n := path.Base(os.Args[0])
	fmt.Fprintf(os.Stderr,
		"Usage:\n"+
			"    %s listclients [<client_id>...]\n"+
			"    %s listcontacts <client_id> [limit]\n"+
			"    %s analysehistory <client_id>\n"+
			"    %s blacklistclients [<client_id>...]\n"+
			"    %s storefile <service_name> <file_name> <file_path>\n"+
			"    %s deletefile <service_name> <file_name>\n"+
			"\n", n, n, n, n, n, n)
}

// Execute examines command line flags and executes one of the standard command line
// actions, as summarized by Usage. It requires a grpc connection to an admin server
// and the command line parameters to interpret.
func Execute(conn *grpc.ClientConn, args ...string) {
	admin := sgrpc.NewAdminClient(conn)

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "A command is required.\n")
		Usage()
		os.Exit(1)
	}

	switch args[0] {
	case "listclients":
		ListClients(admin, args[1:]...)
	case "listcontacts":
		ListContacts(admin, args[1:]...)
	case "analysehistory":
		AnalyseHistory(admin, args[1:]...)
	case "blacklistclients":
		BlacklistClients(admin, args[1:]...)
	case "storefile":
		StoreFile(admin, args[1:]...)
	case "deletefile":
		DeleteFile(admin, args[1:]...)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %v\n", args[0])
		Usage()
		os.Exit(1)
	}
}

// ListClients prints a list of all clients in the fleetspeak system.
func ListClients(c sgrpc.AdminClient, args ...string) {
	var ids [][]byte
	for i, arg := range args {
		id, err := common.StringToClientID(arg)
		if err != nil {
			log.Exitf("Unable to convert [%s] (index %d) to client id: %v", arg, i, err)
		}
		ids = append(ids, id.Bytes())
	}
	ctx := context.Background()
	res, err := c.ListClients(ctx, &spb.ListClientsRequest{ClientIds: ids}, grpc.MaxCallRecvMsgSize(1024*1024*1024))
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
		ts := cl.LastContactTime.AsTime()
		tag := ""
		if cl.Blacklisted {
			tag = " *blacklisted*"
		}
		fmt.Printf("%v %v [%v]%s\n", id, ts.Format(dateFmt), strings.Join(ls, ","), tag)
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

// ListContacts prints a list contacts that the system has recorded for a
// client. args[0] must be a client id. If present, args[1] limits to the most
// recent args[1] contacts.
func ListContacts(c sgrpc.AdminClient, args ...string) {
	if len(args) == 0 || len(args) > 2 {
		Usage()
		os.Exit(1)
	}
	id, err := common.StringToClientID(args[0])
	if err != nil {
		log.Exitf("Unable to parse %s as client id: %v", args[0], err)
	}
	var lim int
	if len(args) == 2 {
		lim, err = strconv.Atoi(args[1])
		if err != nil {
			log.Exitf("Unable to parse %s as a limit: %v", args[1], err)
		}
	}

	ctx := context.Background()
	res, err := c.ListClientContacts(ctx, &spb.ListClientContactsRequest{ClientId: id.Bytes()}, grpc.MaxCallRecvMsgSize(1024*1024*1024))
	if err != nil {
		log.Exitf("ListClientContacts RPC failed: %v", err)
	}
	if len(res.Contacts) == 0 {
		fmt.Println("No contacts found.")
		return
	}

	fmt.Printf("Found %d contacts.\n", len(res.Contacts))

	sort.Sort(byTimestamp(res.Contacts))
	fmt.Printf("%-23s %s", "Timestamp:", "Observed IP:\n")
	for i, con := range res.Contacts {
		if lim > 0 && i > lim {
			break
		}
		if err := con.Timestamp.CheckValid(); err != nil {
			log.Errorf("Unable to parse timestamp for contact: %v", err)
			continue
		}
		ts := con.Timestamp.AsTime()
		fmt.Printf("%s %s\n", ts.Format(dateFmt), con.ObservedAddress)
	}
}

// byTimestamp adapts []*spb.ClientContact for use by sort.Sort. Places most
// recent contacts first.
type byTimestamp []*spb.ClientContact

func (b byTimestamp) Len() int           { return len(b) }
func (b byTimestamp) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byTimestamp) Less(i, j int) bool { return timestamp(b[i]).After(timestamp(b[j])) }

func timestamp(c *spb.ClientContact) time.Time {
	return time.Unix(c.Timestamp.Seconds, int64(c.Timestamp.Nanos))
}

// AnalyseHistory prints a summary analysis of a client's history. args[0] must
// be a client id.
func AnalyseHistory(c sgrpc.AdminClient, args ...string) {
	if len(args) != 1 {
		Usage()
		os.Exit(1)
	}
	id, err := common.StringToClientID(args[0])
	if err != nil {
		log.Exitf("Unable to parse %s as client id: %v", args[0], err)
	}
	ctx := context.Background()
	res, err := c.ListClientContacts(ctx, &spb.ListClientContactsRequest{ClientId: id.Bytes()}, grpc.MaxCallRecvMsgSize(1024*1024*1024))
	if err != nil {
		log.Exitf("ListClientContacts RPC failed: %v", err)
	}
	if len(res.Contacts) == 0 {
		fmt.Println("No contacts found.")
		return
	}
	s, err := history.Summarize(res.Contacts)
	if err != nil {
		log.Exitf("Error creating summary: %v", err)
	}
	fmt.Printf(`Raw Summary:
  First Recorded Contact: %v
  Last Recorded Contact: %v
  Contact Count: %d
  Observed IP Count: %d
  Split Points: %d
  Splits: %d
  Skips: %d
`, s.Start, s.End, s.Count, s.IPCount, s.SplitPoints, s.Splits, s.Skips)
	if s.Splits > 0 {
		fmt.Printf("This client appears to have be restored %d times from %d different backup images.\n", s.Splits, s.SplitPoints)
	}
	if s.Skips > s.Splits {
		fmt.Printf("Observed %d Skips, but only %d splits. The machine may have been cloned.\n", s.Skips, s.Splits)
	}
}

// BlacklistClients blacklists given client ids, forcing any clients using them
// to rekey.
func BlacklistClients(c sgrpc.AdminClient, args ...string) {
	var ids [][]byte
	for i, arg := range args {
		id, err := common.StringToClientID(arg)
		if err != nil {
			log.Exitf("Unable to convert %q (index %d) to client id: %v", arg, i, err)
		}
		ids = append(ids, id.Bytes())
	}
	ctx := context.Background()
	for _, id := range ids {
		if _, err := c.BlacklistClient(ctx, &spb.BlacklistClientRequest{ClientId: id}); err != nil {
			log.Exitf("BlacklistClient RPC failed: %v", err)
		}
	}
}

// StoreFile reads a file from disk and sends it to the server's file store.
// args[0] must be a service name, args[1] the file name to store as, and
// args[2] the path to the file on disk.
func StoreFile(c sgrpc.AdminClient, args ...string) {
	if len(args) != 3 {
		Usage()
		os.Exit(1)
	}
	serviceName := args[0]
	fileName := args[1]
	filePath := args[2]

	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Exitf("Failed to read file [%s]: %v", filePath, err)
	}

	req := &spb.StoreFileRequest{
		ServiceName: serviceName,
		FileName:    fileName,
		Data:        data,
	}

	ctx := context.Background()
	if _, err := c.StoreFile(ctx, req); err != nil {
		log.Exitf("StoreFile RPC failed: %v", err)
	}
	fmt.Printf("Stored file %s/%s to server.\n", serviceName, fileName)
}

// DeleteFile tells the server to delete a file from its file store.
// args[0] must be a service name, args[1] the file name to delete.
func DeleteFile(c sgrpc.AdminClient, args ...string) {
	if len(args) != 2 {
		Usage()
		os.Exit(1)
	}
	serviceName := args[0]
	fileName := args[1]

	req := &spb.DeleteFileRequest{
		ServiceName: serviceName,
		FileName:    fileName,
	}

	ctx := context.Background()
	if _, err := c.DeleteFile(ctx, req); err != nil {
		log.Exitf("DeleteFile RPC failed: %v", err)
	}
	fmt.Printf("Deleted file %s/%s from server.\n", serviceName, fileName)
}
