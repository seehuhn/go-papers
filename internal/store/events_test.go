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

package store

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogEventAppends(t *testing.T) {
	s := testStore(t)
	s.LogEvent(Event{Command: "fetch", Input: "doi", Ref: "10.1/x", Outcome: "ok", Source: "crossref"})
	s.LogEvent(Event{Command: "ingest", Input: "file", Ref: "a.pdf", Outcome: "unidentified"})

	files, err := filepath.Glob(filepath.Join(s.Root, "events", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("want exactly one events file, got %v (%v)", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), lines)
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if e.Command != "fetch" || e.Outcome != "ok" || e.When == "" {
		t.Errorf("event = %+v; When must be auto-filled", e)
	}
}

func TestLogEventBestEffort(t *testing.T) {
	// an unusable store root must neither error nor panic
	s := &Store{Root: string([]byte{0})}
	s.LogEvent(Event{Command: "fetch", Outcome: "ok"})
}
