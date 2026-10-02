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
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestEventLog writes lines to the configured store's event log
// for this machine.
func writeTestEventLog(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "events"), 0o755); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	// test hostnames are plain; eventHostname leaves them unchanged
	path := filepath.Join(dir, "events", host+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEventsJSON(t *testing.T) {
	dir := initStore(t, "")
	writeTestEventLog(t, dir,
		`{"when":"2026-10-01T06:00:00","command":"chain","ref":"w1","outcome":"ok","run":"r1","session":"s","credits":5}`,
		`{"when":"2026-10-01T06:00:01","command":"chain","ref":"w2","outcome":"unresolved","run":"r1","session":"s","credits":9}`,
	)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runEvents([]string{"-json", "-session", "s", "-since", "2026-10-01T00:00:00Z"})
	})
	if runErr != nil {
		t.Fatalf("runEvents: %v", runErr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1: %s", len(got), out)
	}
	o := got[0]
	for _, k := range []string{"run", "command", "ref", "when", "credits", "outcome", "events"} {
		if _, ok := o[k]; !ok {
			t.Errorf("missing key %q in %v", k, o)
		}
	}
	if o["run"] != "r1" || o["command"] != "chain" || o["ref"] != "w1" ||
		o["outcome"] != "unresolved" || o["credits"] != 9.0 || o["events"] != 2.0 {
		t.Errorf("object = %v", o)
	}
	if w, _ := o["when"].(string); !strings.HasPrefix(w, "2026-10-01T06:00:00") || len(w) <= len("2026-10-01T06:00:00") {
		t.Errorf("when = %q, want RFC 3339 with offset", w)
	}
}

func TestEventsJSONEmpty(t *testing.T) {
	initStore(t, "")
	var runErr error
	out := captureStdout(t, func() { runErr = runEvents([]string{"-json"}) })
	if runErr != nil {
		t.Fatalf("runEvents: %v", runErr)
	}
	if out != "[]\n" {
		t.Errorf("output = %q, want %q", out, "[]\n")
	}
}

func TestEventsBadSince(t *testing.T) {
	initStore(t, "")
	if err := runEvents([]string{"-since", "yesterday"}); err == nil {
		t.Fatal("want an error for -since yesterday")
	}
}
