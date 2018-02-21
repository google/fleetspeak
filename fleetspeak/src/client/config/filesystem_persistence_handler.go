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

package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// FilesystemPersistenceHandler defines the filesystem configuration storage strategy.
type FilesystemPersistenceHandler struct {
	configurationPath string
	stateFile         string
}

// NewFilesystemPersistenceHandler instantiates a FilesystemPersistenceHandler.
//
// configurationPath is the location to look for additional configuration
// files. Possible files include:
//
// /communicator.txt    - A text format clpb.CommunicatorConfig, used to tweak communicator behavior.
// /services/<service>  - A binary format SignedClientServiceConfig. One file for each configured service.
//
// All of these files are optional, though Fleetspeak will not be particularly
// useful without at least one configured service.
//
// If stateFile is nonempty, the client will attempt to write persistent state
// to it, in order to preserve identity across restarts.
func NewFilesystemPersistenceHandler(configurationPath, stateFile string) (*FilesystemPersistenceHandler, error) {
	if err := verifyDirectoryPath(configurationPath); err != nil {
		return nil, fmt.Errorf("invalid configuration path: %v", err)
	}
	if stateFile != "" {
		if err := verifyDirectoryPath(filepath.Dir(stateFile)); err != nil {
			return nil, fmt.Errorf("invalid state file directory [%v]: %v", filepath.Dir(stateFile), err)
		}
	}

	return &FilesystemPersistenceHandler{
		configurationPath: configurationPath,
		stateFile:         stateFile,
	}, nil
}

// ReadState implements PersistenceHandler.
func (h *FilesystemPersistenceHandler) ReadState() (*clpb.ClientState, error) {
	if h.stateFile == "" {
		return &clpb.ClientState{}, nil
	}

	b, err := ioutil.ReadFile(h.stateFile)
	if err != nil {
		return nil, err
	}

	ret := &clpb.ClientState{}
	if err := proto.Unmarshal(b, ret); err != nil {
		return nil, fmt.Errorf("unable to parse state file [%s]: %v", h.stateFile, err)
	}

	return ret, nil
}

// WriteState implements PersistenceHandler.
func (h *FilesystemPersistenceHandler) WriteState(s *clpb.ClientState) error {
	if h.stateFile == "" {
		return nil
	}

	b, err := proto.Marshal(s)
	if err != nil {
		log.Fatalf("Unable to serialize state: %v", err)
	}

	tmp := h.stateFile + ".new"
	os.RemoveAll(tmp) // Deliberately ignoring errors.
	if err := ioutil.WriteFile(tmp, b, 0600); err != nil {
		return fmt.Errorf("unable to write new configuration [%s]: %v", tmp, err)
	}
	if err := os.Rename(tmp, h.stateFile); err != nil {
		return fmt.Errorf("unable to rename new confguration [%s]: %v", h.stateFile, err)
	}

	return nil
}

// ReadCommunicatorConfig implements PersistenceHandler.
func (h *FilesystemPersistenceHandler) ReadCommunicatorConfig() (*clpb.CommunicatorConfig, error) {
	if h.configurationPath == "" {
		return nil, errors.New("configuration path not provided, can't read communicator config")
	}
	p := filepath.Join(h.configurationPath, communicatorFilename)
	b, err := ioutil.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("can't read communicator config file [%s]: %v", p, err)
	}

	ret := &clpb.CommunicatorConfig{}
	if err := proto.UnmarshalText(string(b), ret); err != nil {
		return nil, fmt.Errorf("can't parse communicator config [%s]: %v", p, err)
	}

	return ret, nil
}

// ReadSignedServices implements PersistenceHandler.
func (h *FilesystemPersistenceHandler) ReadSignedServices() ([]*fspb.SignedClientServiceConfig, error) {
	p := filepath.Join(h.configurationPath, signedServicesDirname)
	if err := verifyDirectoryPath(p); err != nil {
		return nil, fmt.Errorf("invalid signed services directory path: %v", err)
	}

	fs, err := ls(p)
	if err != nil {
		return nil, fmt.Errorf("unable to list signed services directory [%s]: %v", p, err)
	}

	ret := make([]*fspb.SignedClientServiceConfig, 0)

	for _, f := range fs {
		fp := filepath.Join(p, f)
		b, err := ioutil.ReadFile(fp)
		if err != nil {
			log.Errorf("Unable to read signed service file [%s], ignoring: %v", fp, err)
			continue
		}

		s := &fspb.SignedClientServiceConfig{}
		if err := proto.Unmarshal(b, s); err != nil {
			log.Errorf("Unable to parse signed service file [%s], ignoring: %v", fp, err)
			continue
		}

		ret = append(ret, s)
	}

	return ret, nil
}

// ReadServices implements PersistenceHandler.
func (h *FilesystemPersistenceHandler) ReadServices() ([]*fspb.ClientServiceConfig, error) {
	p := filepath.Join(h.configurationPath, servicesDirname)
	if err := verifyDirectoryPath(p); err != nil {
		return nil, fmt.Errorf("invalid services directory path: %v", err)
	}

	fs, err := ls(p)
	if err != nil {
		return nil, fmt.Errorf("unable to list services directory [%s]: %v", p, err)
	}

	ret := make([]*fspb.ClientServiceConfig, 0)

	for _, f := range fs {
		fp := filepath.Join(p, f)
		b, err := ioutil.ReadFile(fp)
		if err != nil {
			log.Errorf("Unable to read service file [%s], ignoring: %v", fp, err)
			continue
		}

		s := &fspb.ClientServiceConfig{}
		if err := proto.UnmarshalText(string(b), s); err != nil {
			log.Errorf("Unable to parse service file [%s], ignoring: %v", fp, err)
			continue
		}

		ret = append(ret, s)
	}

	return ret, nil
}

func verifyDirectoryPath(dirPath string) error {
	if dirPath == "" {
		return errors.New("no path provided")
	}

	i, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("unable to stat path [%v]: %v", dirPath, err)
	}
	if !i.Mode().IsDir() {
		return fmt.Errorf("the given path [%v] is not a directory", dirPath)
	}

	return nil
}

// ls lists the given directory.
func ls(dirpath string) ([]string, error) {
	d, err := os.Open(dirpath)
	if err != nil {
		return nil, fmt.Errorf("unable to open services path [%s]: %v", dirpath, err)
	}
	defer d.Close()

	fs, err := d.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list files in services path [%s]: %v", dirpath, err)
	}

	return fs, nil
}
