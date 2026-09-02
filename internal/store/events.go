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
	"time"
)

// eventTimeLayout is the layout used for Event.When, matching the layout
// LogEntry.When uses elsewhere in the store.
const eventTimeLayout = "2006-01-02T15:04:05"

// eventUnknownHost is the hostname LogEvent falls back to when
// os.Hostname fails or returns a name that sanitizes to nothing.
const eventUnknownHost = "unknown"

// Event is one line in the store's local event log.
type Event struct {
	When     string `json:"when"`            // 2006-01-02T15:04:05
	Command  string `json:"command"`         // "fetch" | "ingest" | "check" | "search"
	Input    string `json:"input,omitzero"`  // "doi" | "arxiv" | "text" | "file"
	Ref      string `json:"ref,omitzero"`    // the reference, file basename, or search terms
	Outcome  string `json:"outcome"`         // "ok" | "no-hits" | "no-oa-route" | "ambiguous" | "duplicate" | "unidentified" | "mismatch" | "error"
	Source   string `json:"source,omitzero"` // resolving source: "crossref" | "arxiv" | "unpaywall" | ...
	Tier     int    `json:"tier,omitzero"`   // ingest identification tier 1-3
	Hits     int    `json:"hits,omitzero"`   // search result count (outcome "no-hits" marks zero)
	Duration int64  `json:"duration_ms,omitzero"`
	Detail   string `json:"detail,omitzero"` // first line of the error, for outcomes other than "ok"
}

// LogEvent appends e as one JSON line to events/<hostname>.jsonl under the
// store root, creating the directory as needed. Best-effort by contract:
// LogEvent never returns an error and never panics; any failure
// (unwritable store, hostname lookup failure) is silently ignored.
// e.When is filled with the current time if empty.
func (s *Store) LogEvent(e Event) {
	defer func() { recover() }()

	if e.When == "" {
		e.When = time.Now().Format(eventTimeLayout)
	}

	dir := filepath.Join(s.Root, "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	path := filepath.Join(dir, eventHostname()+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(&e)
	if err != nil {
		return
	}
	data = append(data, '\n')

	f.Write(data)
}

// eventHostname returns the sanitized hostname used to name this
// machine's event log file: letters, digits, '.', '-' and '_' pass
// through unchanged, anything else becomes '-'. An empty, unreadable, or
// entirely-sanitized-away hostname falls back to "unknown".
func eventHostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return eventUnknownHost
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	if b.Len() == 0 {
		return eventUnknownHost
	}
	return b.String()
}
