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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Invocation is one run of a paper command, reassembled from the event log:
// the events sharing a run ID, or a single event that carries none.
type Invocation struct {
	Run     string    // run ID; empty for an event logged before run IDs existed
	Command string    // from the first event
	Ref     string    // from the first event
	Outcome string    // from the last event
	When    time.Time // from the first event, in local time
	Credits *float64  // from the last event; nil when that event has none
	Events  int       // number of events in the invocation
}

// Invocations returns the invocations recorded in this machine's event log,
// in the order their first events appear in the log. A non-empty session
// keeps only invocations whose events carry that session, and a non-zero
// since keeps only invocations that began at or after since; with since set,
// an event whose time does not parse is skipped. Blank and malformed lines
// are skipped. A missing log yields an empty slice and no error.
func (s *Store) Invocations(session string, since time.Time) ([]Invocation, error) {
	out := []Invocation{}

	path := filepath.Join(s.Root, "events", eventHostname()+".jsonl")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	byRun := make(map[string]int) // run ID -> index in out
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if session != "" && e.Session != session {
			continue
		}
		when, err := time.ParseInLocation(eventTimeLayout, e.When, time.Local)
		if err != nil && !since.IsZero() {
			continue
		}

		if i, ok := byRun[e.Run]; ok && e.Run != "" {
			inv := &out[i]
			inv.Outcome = e.Outcome
			inv.Credits = e.Credits
			inv.Events++
			continue
		}
		if e.Run != "" {
			byRun[e.Run] = len(out)
		}
		out = append(out, Invocation{
			Run:     e.Run,
			Command: e.Command,
			Ref:     e.Ref,
			Outcome: e.Outcome,
			When:    when,
			Credits: e.Credits,
			Events:  1,
		})
	}

	if !since.IsZero() {
		kept := out[:0]
		for _, inv := range out {
			if !inv.When.Before(since) {
				kept = append(kept, inv)
			}
		}
		out = kept
	}
	return out, nil
}
