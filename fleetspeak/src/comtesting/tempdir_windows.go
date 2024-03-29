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

//go:build windows

package comtesting

import (
	"os"

	log "github.com/golang/glog"
)

func cleanupTempDir() {
	if tempDir == "" {
		return
	}

	if err := os.RemoveAll(tempDir); err != nil {
		log.Errorf("Failed to cleanup temp dir; os.RemoveAll(%q): %v", tempDir, err)
		return
	}

	tempDir = ""
}
