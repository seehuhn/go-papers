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
	"net/http"
	"os"
	"strings"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{"audit", "check a bibliography against the store and the sources", runAudit})
}

// runAudit implements the "paper audit" command: it parses a .bib file,
// matches each reference against the store (the authority for anything it
// holds), and reports where the two disagree. It never writes to the
// store — the store is read for matching, and (with -online, in a later
// task) the network is read for verification, but nothing found here is
// ever saved back.
func runAudit(args []string) error {
	fs, storeFlag := newFlagSet("audit")
	online := fs.Bool("online", false, "re-verify store-held entries against their sources too")
	asJSON := fs.Bool("json", false, "emit the report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("audit: parsing arguments: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("audit: usage: paper audit [-online] [-json] <refs.bib>")
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	defer f.Close()
	entries, err := bibtex.Parse(f)
	if err != nil {
		return fmt.Errorf("audit: %s: %w", fs.Arg(0), err)
	}

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	papers, err := s.LoadAll()
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}

	a := &auditor{papers: papers, email: cfg.Email, online: *online,
		api: &http.Client{Timeout: apiTimeout}}
	report := a.run(entries)

	if *asJSON {
		out, err := renderJSON(report)
		if err != nil {
			return fmt.Errorf("audit: %w", err)
		}
		fmt.Print(out)
		return nil
	}
	fmt.Print(renderProse(report))
	return nil
}

// auditor checks each reference in turn. It never writes: the store is
// read for matching and the network is read for verification, and
// everything found goes into the report for an agent to act on.
type auditor struct {
	papers []*store.Paper
	email  string
	online bool
	api    *http.Client
}

// run checks every parsed bib entry against the store — matching it and,
// when matched, diffing it against the store's version, plus the quality
// bar every entry is held to regardless of a match — and then, unless the
// store already confirmed it, against the online sources via verify.
func (a *auditor) run(entries []bibtex.KeyedEntry) *auditReport {
	r := &auditReport{}
	for _, e := range entries {
		item := auditEntry{Key: e.Key, Line: e.Line, Existence: "unchecked"}
		item.Problems = append(item.Problems, qualityProblems(e)...)
		if p := matchStore(a.papers, e); p != nil {
			item.StoreKey = p.Key
			item.Holdings = p.Holdings
			if p.Audit != nil {
				item.Claims = len(p.Audit.Claims)
			}
			item.Problems = append(item.Problems, diffAgainstStore(e, p)...)
			if p.Status == "clean" {
				item.Existence = "confirmed"
			}
		}
		// The store is the authority for what it holds, so a confirmed entry
		// is not re-resolved unless -online says the report must not depend
		// on how fresh the store is. And even then, the store is itself a
		// source — the paper is physically held — so an online result can
		// add doubt as a problem note but must never outvote possession: a
		// store-confirmed entry is never demoted by verify.
		switch {
		case item.Existence != "confirmed":
			item.Existence, item.Candidates = a.verify(e)
		case a.online:
			if verdict, cands := a.verify(e); verdict != "confirmed" {
				item.Problems = append(item.Problems, fmt.Sprintf("online re-verification: %s", verdict))
				item.Candidates = cands
			}
		}
		r.Entries = append(r.Entries, item)
	}
	return r
}

// qualityProblems applies the spec's quality bar to a bibliography entry:
// the required fields for its type, and a DOI on anything that should have
// one. It deliberately reuses the store's own tables so the bar is the same
// one `paper check` applies.
func qualityProblems(e bibtex.KeyedEntry) []string {
	var out []string
	groups, known := bibtex.RequiredFields[e.Entry.Type]
	if !known {
		return []string{fmt.Sprintf("unknown entry type %q", e.Entry.Type)}
	}
	for _, group := range groups {
		if !anyPresent(e.Entry.Fields, group) {
			out = append(out, fmt.Sprintf("missing required field %s", strings.Join(group, " or ")))
		}
	}
	if e.Entry.Type == "article" && e.Entry.Fields["doi"] == "" {
		out = append(out, "no doi")
	}
	return out
}

func anyPresent(fields map[string]string, group []string) bool {
	for _, name := range group {
		if strings.TrimSpace(fields[name]) != "" {
			return true
		}
	}
	return false
}
