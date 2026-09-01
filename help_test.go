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
	"strings"
	"testing"
)

func TestHelpPerCommand(t *testing.T) {
	for _, c := range commands {
		out := captureStdout(t, func() {
			if err := dispatch([]string{"help", c.name}); err != nil {
				t.Errorf("help %s: %v", c.name, err)
			}
		})
		if !strings.Contains(out, "usage: paper "+c.name) {
			t.Errorf("help %s: output lacks usage line; got %q", c.name, out)
		}
		if len(out) < 200 {
			t.Errorf("help %s: output suspiciously short (%d bytes); per-command help should document behavior, not just flags", c.name, len(out))
		}
	}
}

func TestHelpSchemaTopic(t *testing.T) {
	out := captureStdout(t, func() {
		if err := dispatch([]string{"help", "schema"}); err != nil {
			t.Errorf("help schema: %v", err)
		}
	})
	for _, want := range []string{"paper.json", "backslash", "audit", "claims", "holdings"} {
		if !strings.Contains(out, want) {
			t.Errorf("help schema: output lacks %q", want)
		}
	}
}

func TestHelpUnknownTopic(t *testing.T) {
	err := dispatch([]string{"help", "no-such-topic"})
	if err == nil || !strings.Contains(err.Error(), "no-such-topic") {
		t.Errorf("help no-such-topic: got %v, want error naming the topic", err)
	}
}

func TestHelpListMentionsTopics(t *testing.T) {
	out := captureStdout(t, func() {
		if err := dispatch([]string{"help"}); err != nil {
			t.Errorf("help: %v", err)
		}
	})
	if !strings.Contains(out, "schema") {
		t.Error("top-level help does not list the schema topic")
	}
	if !strings.Contains(out, "paper help <") {
		t.Error("top-level help does not point at per-command help")
	}
}

func TestDashHShowsLongHelp(t *testing.T) {
	// -h routes through the flag package, which prints usage via
	// fs.Output (stderr by default); the command must still return nil.
	out := captureStderr(t, func() {
		if err := dispatch([]string{"search", "-h"}); err != nil {
			t.Errorf("search -h: %v", err)
		}
	})
	if !strings.Contains(out, "usage: paper search") {
		t.Errorf("search -h: output lacks long help; got %q", out)
	}
}
