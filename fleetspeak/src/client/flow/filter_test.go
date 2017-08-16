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

package flow

import (
	"testing"
)

func TestFilter(t *testing.T) {
	f := NewFilter()

	l, m, h := f.Get()
	if l != false || m != false || h != false {
		t.Errorf("Filter should start (false,false,false), got (%v, %v, %v)", l, m, h)
	}

	f.Set(true, false, false)
	l, m, h = f.Get()
	if l != true || m != false || h != false {
		t.Errorf("Filter should be (true,false,false), got (%v, %v, %v)", l, m, h)
	}

	f.Set(true, true, false)
	l, m, h = f.Get()
	if l != true || m != true || h != false {
		t.Errorf("Filter should be (true,true,false), got (%v, %v, %v)", l, m, h)
	}

	f.Set(true, true, true)
	l, m, h = f.Get()
	if l != true || m != true || h != true {
		t.Errorf("Filter should be (true,true,true), got (%v, %v, %v)", l, m, h)
	}
}
