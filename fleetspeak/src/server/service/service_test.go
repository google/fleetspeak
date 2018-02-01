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

package service

import (
	"errors"
	"testing"
)

type netErr struct {
	msg        string
	temp, time bool
}

func (e netErr) Error() string {
	return e.msg
}

func (e netErr) Temporary() bool {
	return e.temp
}

func (e netErr) Timeout() bool {
	return e.time
}

func TestIsTemporary(t *testing.T) {
	for _, tc := range []struct {
		e    error
		want bool
	}{
		{
			e:    errors.New("a permanent error"),
			want: false,
		},
		{
			e:    TemporaryError{errors.New("a temporary error")},
			want: true,
		},
		{
			e:    &TemporaryError{errors.New("a referenced temporary error")},
			want: true,
		},
		{
			e:    netErr{msg: "A generic net.Error"},
			want: false,
		},
		{
			e:    netErr{msg: "A temporary net.Error", temp: true},
			want: true,
		},
		{
			e:    netErr{msg: "A timeout net.Error", time: true},
			want: true,
		},
	} {
		got := IsTemporary(tc.e)
		if got != tc.want {
			t.Errorf("IsTemporary(%T)=%v, but want %v", tc.e, got, tc.want)
		}
	}
}
