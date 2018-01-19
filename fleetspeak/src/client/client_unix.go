// Copyright 2018 Google Inc.
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

// +build linux darwin

package client

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	log "github.com/golang/glog"
)

// readServices reads all service files from the given filesystem directory path.
func readServices(configurationPath string) ([]serviceBytes, error) {
	p := path.Join(configurationPath, "services")
	i, err := os.Stat(configurationPath)
	if err != nil {
		return nil, fmt.Errorf("unable to stat services path [%s]: %v", p, err)
	}
	if !i.Mode().IsDir() {
		return nil, fmt.Errorf("services path [%s] is not a directory.", p)
	}

	d, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("unable to open services path [%s]: %v", p, err)
	}
	defer d.Close()

	fs, err := d.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list files in services path [%s]: %v", p, err)
	}

	ret := make([]serviceBytes, 0)

	for _, f := range fs {
		fp := path.Join(p, f)
		b, err := ioutil.ReadFile(fp)
		if err != nil {
			log.Warningf("Unable to read service file [%s], ignoring: %v", fp, err)
			continue
		}

		ret = append(ret, serviceBytes{
			path: fp,
			bytes: b,
		})
	}

	return ret, nil
}
