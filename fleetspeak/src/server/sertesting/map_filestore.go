// Copyright 2026 Google Inc.
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

package sertesting

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path"
	"sync"
	"testing/fstest"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
)

// MapFileStore implements db.FileStore using fstest.MapFS.
type MapFileStore struct {
	mu sync.Mutex
	fs fstest.MapFS
}

// NewMapFileStore creates a new MapFileStore.
func NewMapFileStore() *MapFileStore {
	return &MapFileStore{
		fs: make(fstest.MapFS),
	}
}

// StoreFile stores a file.
func (m *MapFileStore) StoreFile(ctx context.Context, service, name string, data io.Reader) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	p := path.Join(service, name)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.fs[p] = &fstest.MapFile{Data: b, ModTime: time.Now()}
	return nil
}

// DeleteFile deletes a file.
func (m *MapFileStore) DeleteFile(ctx context.Context, service, name string) error {
	p := path.Join(service, name)

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.fs, p)
	return nil
}

// StatFile stats a file.
func (m *MapFileStore) StatFile(ctx context.Context, service, name string) (time.Time, error) {
	p := path.Join(service, name)

	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.fs[p]
	if !ok {
		return time.Time{}, fs.ErrNotExist
	}
	return f.ModTime, nil
}

// ReadFile reads a file.
func (m *MapFileStore) ReadFile(ctx context.Context, service, name string) (db.ReadSeekerCloser, time.Time, error) {
	p := path.Join(service, name)

	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.fs[p]
	if !ok {
		return nil, time.Time{}, fs.ErrNotExist
	}
	return db.NOOPCloser{ReadSeeker: bytes.NewReader(f.Data)}, f.ModTime, nil
}

// SetFile sets a file's content directly, bypasses StoreFile.
func (m *MapFileStore) SetFile(service, name string, data []byte, modTime time.Time) {
	p := path.Join(service, name)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.fs[p] = &fstest.MapFile{Data: data, ModTime: modTime}
}
