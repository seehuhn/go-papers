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
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"
)

// writeEventLog writes lines to this machine's event log in a fresh store.
func writeEventLog(t *testing.T, lines ...string) *Store {
	t.Helper()
	s := testStore(t)
	dir := filepath.Join(s.Root, "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var data string
	for _, l := range lines {
		data += l + "\n"
	}
	path := filepath.Join(dir, eventHostname()+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInvocationsGroupByRun(t *testing.T) {
	s := writeEventLog(t,
		`{"when":"2026-10-01T06:00:00","command":"chain","ref":"w1","outcome":"not-found","run":"r1","credits":16}`,
		`{"when":"2026-10-01T06:00:01","command":"chain","ref":"w2","outcome":"ok","run":"r1","credits":56}`,
		`{"when":"2026-10-01T06:00:02","command":"discover","ref":"x","outcome":"ok","run":"r2"}`,
		`{"when":"2026-10-01T06:00:03","command":"chain","ref":"w3","outcome":"unresolved","run":"r1","credits":106}`,
	)
	got, err := s.Invocations("", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d invocations, want 2: %+v", len(got), got)
	}
	r1 := got[0]
	if r1.Run != "r1" || r1.Command != "chain" || r1.Ref != "w1" || r1.Outcome != "unresolved" || r1.Events != 3 {
		t.Errorf("r1 = %+v", r1)
	}
	if r1.Credits == nil || *r1.Credits != 106 {
		t.Errorf("r1 credits = %v, want 106", r1.Credits)
	}
	if got[1].Command != "discover" || got[1].Credits != nil {
		t.Errorf("r2 = %+v", got[1])
	}
}

func TestInvocationsLegacyEvents(t *testing.T) {
	s := writeEventLog(t,
		`{"when":"2026-10-01T06:00:00","command":"fetch","outcome":"ok"}`,
		`{"when":"2026-10-01T06:00:01","command":"fetch","outcome":"ok"}`,
	)
	got, err := s.Invocations("", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d invocations, want 2", len(got))
	}
}

func TestInvocationsSessionFilter(t *testing.T) {
	s := writeEventLog(t,
		`{"when":"2026-10-01T06:00:00","command":"fetch","outcome":"ok","run":"r1","session":"a"}`,
		`{"when":"2026-10-01T06:00:01","command":"search","outcome":"ok","run":"r2","session":"b"}`,
	)
	got, err := s.Invocations("a", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Run != "r1" {
		t.Fatalf("got %+v, want only r1", got)
	}
}

func TestInvocationsSinceAcrossZones(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skip(err)
	}
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })

	s := writeEventLog(t,
		`{"when":"2026-10-01T06:43:00","command":"fetch","outcome":"ok","run":"early"}`,
		`{"when":"2026-10-01T06:45:00","command":"fetch","outcome":"ok","run":"late"}`,
	)
	since, err := time.Parse(time.RFC3339, "2026-10-01T05:44:01.123Z")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Invocations("", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Run != "late" {
		t.Fatalf("got %+v, want only late", got)
	}
}

func TestInvocationsSkipsBadLines(t *testing.T) {
	s := writeEventLog(t,
		`{"when":"2026-10-01T06:00:00","command":"fetch","outcome":"ok","run":"r1"}`,
		``,
		`not json`,
		`{"when":"2026-10-01T06:00:01","command":"search","outcome":"ok","run":"r2"}`,
	)
	got, err := s.Invocations("", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d invocations, want 2", len(got))
	}
}

func TestInvocationsMissingLog(t *testing.T) {
	s := testStore(t)
	got, err := s.Invocations("", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}
