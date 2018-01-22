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

// +build windows

package client

import (
	"fmt"
	"strings"

	log "github.com/golang/glog"
	"golang.org/x/sys/windows/registry"
)

// readServices reads all service entries from the given registry key path.
func readServices(configurationPath string) ([]serviceBytes, error) {
	const prefix = `HKEY_LOCAL_MACHINE\`

	if !strings.HasPrefix(configurationPath, prefix) {
		return nil, fmt.Errorf("invalid config path [%s], only registry paths starting with %s are supported", configurationPath, prefix)
	}

	fullPath := configurationPath + `\services`
	p := fullPath[len(prefix):]

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, p, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("unable to open services key path [%s]: %v", p, err)
	}
	defer k.Close()

	vs, err := k.ReadValueNames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in services key path [%s]: %v", p, err)
	}

	ret := make([]serviceBytes, 0)

	for _, v := range vs {
		prettyPath := fullPath + " -> " + v

		b, _, err := k.GetBinaryValue(v)
		if err != nil {
			log.Warningf("Unable to read service value [%s], ignoring; note that only binary (REG_BINARY) values are supported: %v", prettyPath, err)
			continue
		}

		ret = append(ret, serviceBytes{
			path:  prettyPath,
			bytes: b,
		})
	}

	return ret, nil
}
