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

// +build windows

// Package hashpipe implements a name randomization mechanism over Windows
// named pipes.
package hashpipe

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// ListenPipe refines winio.ListenPipe by providing a crypto-secure random name
// for the pipe and returning the name alongside the normal net.Listener.
func ListenPipe(c *winio.PipeConfig) (l net.Listener, path string, err error) {
	if path, err = randomizePath(); err != nil {
		return
	}

	l, err = winio.ListenPipe(path, c)
	return
}

func randomizePath() (string, error) {
	// 96 bytes of information gives us 128 base-64 characters. Windows named
	// pipes support paths of up to 256 characters. See:
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa365783(v=vs.85).aspx
	randBuf := make([]byte, 96)

	if n, err := rand.Read(randBuf); err != nil {
		return "", fmt.Errorf("error in rand.Read (%d bytes read): %v", n, err)
	}

	randB64String := base64.URLEncoding.EncodeToString(randBuf)

	pipePath := `\\.\pipe\` + randB64String
	return pipePath, nil
}
