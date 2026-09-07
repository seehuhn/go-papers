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
	"errors"
	"flag"
	"fmt"
	"strings"

	"seehuhn.de/go/paper/internal/store"
)

const eventHelp = `usage: paper event [options] <command>

Append one line to the store's event log, events/<hostname>.jsonl. The
log is schemaless JSONL - one JSON object per line, keyed by hostname
so that machines sharing a synced store never write to the same file.
<command> is a short free-form word naming the thing that happened,
such as "dispatch" or "librarian".

The tool's own commands (fetch, ingest, search, check, ...) already log
their own outcomes automatically; "paper event" is for telemetry from
outside the tool: a hook script or the librarian agent recording an
agent dispatch, a hunt summary, or a hand-off between agents.

-detail and -ref are sanitised before being recorded: whitespace runs,
including newlines, collapse to a single space, the result is trimmed,
and it is truncated to 500 characters (with a trailing "…") - so a
hook can pass a raw prompt excerpt without corrupting the log.

Writing the event is best-effort, exactly like the tool's own event
logging: a failure to write (unwritable store, hostname lookup
failure) is silently ignored and never fails the command. Only a
missing or extra <command> argument is an error.

options:
    -detail <s>     free-text detail, sanitised and truncated
    -duration <ms>  how long the logged action took, in milliseconds
    -input <s>      the kind of input the action received
    -outcome <s>    how the action ended (default "ok")
    -ref <s>        a reference for the action, sanitised and truncated
    -source <s>     the source or agent that produced the outcome
    -store <dir>    path to the paper store (overrides the configured store)
`

func init() {
	commands = append(commands, command{
		name: "event",
		desc: "append a line to the store's event log",
		help: eventHelp,
		run:  runEvent,
	})
}

// eventTextMax bounds the length, in runes, of the sanitised -detail and
// -ref values, so that a hook passing a raw prompt excerpt cannot bloat
// the event log.
const eventTextMax = 500

// sanitiseEventText collapses every whitespace run in s (including
// newlines) to a single space, trims the result, and truncates it to
// eventTextMax runes, appending "…" when truncation happened. It exists
// so that "paper event" can safely carry free text such as a raw prompt
// excerpt in a single JSONL line.
func sanitiseEventText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > eventTextMax {
		s = string(r[:eventTextMax-1]) + "…"
	}
	return s
}

// runEvent implements the "paper event" command: it appends one line to
// the store's event log via (*store.Store).LogEvent, for telemetry that
// originates outside the tool itself (hook scripts, the librarian
// agent). The only error this returns is a usage error over the
// positional <command> argument; opening the store or writing the log
// never fails the command, matching LogEvent's own best-effort contract.
func runEvent(args []string) error {
	fs, storeFlag := newFlagSet("event")
	inputFlag := fs.String("input", "", "the kind of input the action received")
	refFlag := fs.String("ref", "", "a reference for the action")
	outcomeFlag := fs.String("outcome", "ok", "how the action ended")
	sourceFlag := fs.String("source", "", "the source or agent that produced the outcome")
	durationFlag := fs.Int64("duration", 0, "how long the action took, in milliseconds")
	detailFlag := fs.String("detail", "", "free-text detail")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("event: parsing arguments: %w", err)
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("event: usage: paper event [options] <command>")
	}
	cmdWord := fs.Arg(0)

	s, _, err := openStore(*storeFlag)
	if err != nil {
		// Best-effort: a hook script must never fail because the store
		// is unconfigured or unreachable.
		return nil
	}

	s.LogEvent(store.Event{
		Command:  cmdWord,
		Input:    *inputFlag,
		Ref:      sanitiseEventText(*refFlag),
		Outcome:  *outcomeFlag,
		Source:   *sourceFlag,
		Duration: *durationFlag,
		Detail:   sanitiseEventText(*detailFlag),
	})
	return nil
}
