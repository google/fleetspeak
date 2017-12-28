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

// Package main implements a general command line interface which performs
// administrative actions on a fleetspeak installation.
//
// Most functionality is implemented by the cli sub-package. Installations may
// want to branch this file to adjust the default admin_addr flag, dial
// parameters and similar.
package main

import (
	"flag"

	log "github.com/golang/glog"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/admin/cli"
)

var adminAddr = flag.String("admin_addr", "localhost:6061", "Address for the admin server.")

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*adminAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Exitf("Unable to connect to fleetspeak admin interface [%v]: %v", *adminAddr, err)
	}

	cli.Execute(conn, flag.Args()...)
}
