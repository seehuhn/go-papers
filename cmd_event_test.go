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
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/store"
)

// readEventLines returns the events log for the fixture store rooted at
// dir, split into non-empty lines, so a test can assert on the number of
// lines written as well as their content.
func readEventLines(t *testing.T, dir string) []string {
	t.Helper()
	log := readEventLog(t, dir)
	log = strings.TrimRight(log, "\n")
	if log == "" {
		return nil
	}
	return strings.Split(log, "\n")
}

func TestEventWritesOneLine(t *testing.T) {
	_, dir := fixtureStore(t)

	captureStdout(t, func() {
		if err := dispatch([]string{"event",
			"-input", "prompt",
			"-ref", "some reference",
			"-outcome", "start",
			"-source", "hook",
			"-duration", "150",
			"-detail", "some detail",
			"dispatch",
		}); err != nil {
			t.Fatalf("event: %v", err)
		}
	})

	lines := readEventLines(t, dir)
	if len(lines) != 1 {
		t.Fatalf("want exactly one event line, got %d: %v", len(lines), lines)
	}

	var e store.Event
	if err := json.Unmarshal([]byte(lines[0]), &e, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("decoding event line: %v", err)
	}

	if e.When == "" {
		t.Error("event.When is empty")
	}
	if e.Command != "dispatch" {
		t.Errorf("Command = %q, want %q", e.Command, "dispatch")
	}
	if e.Input != "prompt" {
		t.Errorf("Input = %q, want %q", e.Input, "prompt")
	}
	if e.Ref != "some reference" {
		t.Errorf("Ref = %q, want %q", e.Ref, "some reference")
	}
	if e.Outcome != "start" {
		t.Errorf("Outcome = %q, want %q", e.Outcome, "start")
	}
	if e.Source != "hook" {
		t.Errorf("Source = %q, want %q", e.Source, "hook")
	}
	if e.Duration != 150 {
		t.Errorf("Duration = %d, want 150", e.Duration)
	}
	if e.Detail != "some detail" {
		t.Errorf("Detail = %q, want %q", e.Detail, "some detail")
	}
}

func TestEventSanitisesDetailAndRef(t *testing.T) {
	_, dir := fixtureStore(t)

	messyDetail := "line one\n\n  line two\twith\ttabs   and   spaces\n"
	longRef := strings.Repeat("x", 600)

	captureStdout(t, func() {
		if err := dispatch([]string{"event",
			"-detail", messyDetail,
			"-ref", longRef,
			"librarian",
		}); err != nil {
			t.Fatalf("event: %v", err)
		}
	})

	lines := readEventLines(t, dir)
	if len(lines) != 1 {
		t.Fatalf("want exactly one event line, got %d: %v", len(lines), lines)
	}

	var e store.Event
	if err := json.Unmarshal([]byte(lines[0]), &e, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("decoding event line: %v", err)
	}

	wantDetail := "line one line two with tabs and spaces"
	if e.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", e.Detail, wantDetail)
	}

	wantRefLen := 500
	if r := []rune(e.Ref); len(r) != wantRefLen {
		t.Errorf("Ref length = %d, want %d", len(r), wantRefLen)
	}
	if !strings.HasSuffix(e.Ref, "…") {
		t.Errorf("Ref = %q, want a truncated value ending in …", e.Ref)
	}
	wantRef := strings.Repeat("x", 499) + "…"
	if e.Ref != wantRef {
		t.Errorf("Ref = %q, want %q", e.Ref, wantRef)
	}
}

func TestEventRequiresCommandArgument(t *testing.T) {
	fixtureStore(t)

	if err := dispatch([]string{"event"}); err == nil {
		t.Error("expected a usage error with no <command> argument")
	}

	if err := dispatch([]string{"event", "one", "two"}); err == nil {
		t.Error("expected a usage error with more than one <command> argument")
	}
}
