// seehuhn.de/go/paper - tools for managing a store of scientific papers
// Copyright (C) 2026  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readEventLog returns the concatenated contents of every events file in
// the store rooted at dir, or "" when none exists.
func readEventLog(t *testing.T, dir string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "events", "*.jsonl"))
	if err != nil {
		t.Fatalf("globbing events: %v", err)
	}
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		b.Write(data)
	}
	return b.String()
}

func TestSearchLogsEvent(t *testing.T) {
	s, dir := fixtureStore(t)
	saveSearchFixture(t, s)

	captureStdout(t, func() {
		if err := dispatch([]string{"search", "hoeffding"}); err != nil {
			t.Fatalf("search: %v", err)
		}
	})

	log := readEventLog(t, dir)
	if !strings.Contains(log, `"command":"search"`) {
		t.Fatalf("no search event logged; log: %q", log)
	}
	if !strings.Contains(log, `"ref":"hoeffding"`) {
		t.Errorf("search event does not record the terms; log: %q", log)
	}
	if !strings.Contains(log, `"outcome":"ok"`) {
		t.Errorf("search event outcome is not ok; log: %q", log)
	}
	if !strings.Contains(log, `"hits":1`) {
		t.Errorf("search event does not record the hit count; log: %q", log)
	}
}

func TestSearchLogsNoHits(t *testing.T) {
	s, dir := fixtureStore(t)
	saveSearchFixture(t, s)

	captureStdout(t, func() {
		if err := dispatch([]string{"search", "nonexistent-topic-xyzzy"}); err != nil {
			t.Fatalf("search: %v", err)
		}
	})

	log := readEventLog(t, dir)
	if !strings.Contains(log, `"outcome":"no-hits"`) {
		t.Errorf("zero-hit search not distinguishable; log: %q", log)
	}
}
