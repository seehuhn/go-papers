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
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

const eventsHelp = `usage: paper events [options]

List the paper invocations recorded in this machine's event log. An
invocation is one run of a paper command; the events it logged share a
run ID and are reported as a single entry, with the command and
reference of its first event and the outcome of its last. The command
only reads the log: it writes no events and uses no network.

Plain output is one tab-separated line per invocation:

    <run>  <command>  <ref>  <when>  <credits>  <outcome>  <events>

With -json the invocations are printed instead as a JSON array of
objects with the keys run, command, ref, when (RFC 3339, local offset),
credits (omitted when unknown), outcome and events. Use -json whenever
the output is parsed rather than read.

options:
    -json          print invocations as a JSON array
    -since <t>     only invocations that began at or after <t> (RFC 3339)
    -session <s>   only invocations made in session <s> (PAPER_SESSION)
    -store <dir>   path to the paper store (overrides the configured store)
`

func init() {
	commands = append(commands, command{
		name: "events",
		desc: "list invocations from the event log",
		help: eventsHelp,
		run:  runEvents,
	})
}

// eventsResult is the JSON representation of one invocation.
type eventsResult struct {
	Run     string   `json:"run"`
	Command string   `json:"command"`
	Ref     string   `json:"ref"`
	When    string   `json:"when"`
	Credits *float64 `json:"credits,omitzero"`
	Outcome string   `json:"outcome"`
	Events  int      `json:"events"`
}

// runEvents implements the "paper events" command: it prints the
// invocations in the local event log (see store.Invocations), optionally
// narrowed by -session and -since, as tab-separated lines or, with -json,
// as a JSON array. It never writes to the store.
func runEvents(args []string) error {
	fs, storeFlag := newFlagSet("events")
	jsonFlag := fs.Bool("json", false, "print invocations as a JSON array")
	sinceFlag := fs.String("since", "", "only invocations that began at or after this time (RFC 3339)")
	sessionFlag := fs.String("session", "", "only invocations made in this session")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("events: parsing arguments: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("events: unexpected argument %q", fs.Arg(0))
	}

	var since time.Time
	if *sinceFlag != "" {
		var err error
		since, err = time.Parse(time.RFC3339, *sinceFlag)
		if err != nil {
			return fmt.Errorf("events: invalid -since %q; want RFC 3339, e.g. 2026-10-01T05:44:01Z", *sinceFlag)
		}
	}

	s, _, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("events: %w", err)
	}
	invs, err := s.Invocations(*sessionFlag, since)
	if err != nil {
		return fmt.Errorf("events: %w", err)
	}

	if *jsonFlag {
		results := make([]eventsResult, 0, len(invs))
		for _, inv := range invs {
			results = append(results, eventsResult{
				Run:     inv.Run,
				Command: inv.Command,
				Ref:     inv.Ref,
				When:    inv.When.Format(time.RFC3339),
				Credits: inv.Credits,
				Outcome: inv.Outcome,
				Events:  inv.Events,
			})
		}
		data, err := json.Marshal(results, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("events: encoding JSON: %w", err)
		}
		os.Stdout.Write(data)
		fmt.Println()
		return nil
	}

	for _, inv := range invs {
		credits := ""
		if inv.Credits != nil {
			credits = strconv.FormatFloat(*inv.Credits, 'f', -1, 64)
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			inv.Run, inv.Command, inv.Ref, inv.When.Format(time.RFC3339), credits, inv.Outcome, inv.Events)
	}
	return nil
}
