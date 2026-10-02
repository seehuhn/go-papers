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
	"regexp"
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

// readEventLines returns the decoded raw lines of the store's event log.
func readEventLines(t *testing.T, s *Store) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(s.Root, "events", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("want exactly one events file, got %v (%v)", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestLogEventStampsInvocation(t *testing.T) {
	t.Cleanup(func() { SetInvocation("", "") })
	SetInvocation("0123456789abcdef", "sess-1")

	s := testStore(t)
	s.LogEvent(Event{Command: "fetch", Outcome: "ok"})
	s.LogEvent(Event{Command: "search", Outcome: "no-hits"})

	lines := readEventLines(t, s)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	for _, line := range lines {
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.Run != "0123456789abcdef" || e.Session != "sess-1" {
			t.Errorf("got run %q session %q in %s", e.Run, e.Session, line)
		}
	}
}

func TestLogEventWithoutSession(t *testing.T) {
	t.Cleanup(func() { SetInvocation("", "") })
	SetInvocation("0123456789abcdef", "")

	s := testStore(t)
	s.LogEvent(Event{Command: "fetch", Outcome: "ok"})

	line := readEventLines(t, s)[0]
	if strings.Contains(line, `"session"`) {
		t.Errorf("session key present: %s", line)
	}
	if !strings.Contains(line, `"run":"0123456789abcdef"`) {
		t.Errorf("run missing: %s", line)
	}
}

func TestNewRunID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{16}$`)
	a, b := NewRunID(), NewRunID()
	if a == b {
		t.Errorf("two run IDs are equal: %s", a)
	}
	for _, id := range []string{a, b} {
		if !re.MatchString(id) {
			t.Errorf("run ID %q does not match", id)
		}
	}
}
