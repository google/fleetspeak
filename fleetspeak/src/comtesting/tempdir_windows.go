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

package comtesting

import (
	"os"
	"path/filepath"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/windows/regutil"
)

const tempRegPrefix = `HKEY_LOCAL_MACHINE\SOFTWARE\FleetspeakDevelopment`

var tempReg = ""

// GetTempRegKey returns the name of a temporary registry key.  It uses the name of the test that is being run as part of the name of the key.
//
// The second returned value is a cleanup function.
func GetTempRegKey(testName string) (string, func()) {
	tempReg = filepath.Join(tempRegPrefix, testName+"_")
	log.Infof("Using temp registry path: %s", tempReg)
	return tempReg, cleanupTempReg
}

func cleanupTempReg() {
	if tempReg == "" {
		return
	}

	err := regutil.DeleteKey(tempRegPrefix)
	if err != nil {
		log.Errorf("Failed to cleanup temp registry [%s]: %v", tempRegPrefix, err)
		return
	}

	tempReg = ""
}

func getTempDirOrRegKey(testName string) (string, func()) {
	return GetTempRegKey(testName)
}

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
