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

const auditHelp = `usage: paper audit [options] <refs.bib>

Check a bibliography file against the store and, where the store does
not already know, against the online sources (Crossref, arXiv, zbMATH,
DBLP). Audit is read-only: it never writes to the store.

Each reference is matched against the store by DOI, then arXiv ID,
then title; a title match needs similarity of at least 0.9 AND
corroboration (first-author surname, or year within one) - a title
alone never counts as a match. A match against a clean store entry is
authoritative: the store has already passed "paper check", so its
fields win over the .bib's.

Every reference gets one of four existence verdicts:

    confirmed   an identifier resolved, the store held a clean entry,
                or a corroborated search hit cleared the title bar
    unverified  candidates exist but none clears the bar; up to three
                are listed with scores - a request for judgement, not
                a finding either way
    notFound    every source that could answer, answered, and returned
                nothing - the only verdict that supports calling a
                reference invented
    unchecked   every source that could have answered errored out, so
                nothing was established either way; not a finding

The prose report prints notFound as "not found", and unchecked entries
under "Not checked:"; -json keeps the camelCase verdict names, so
match against those when scripting. A "Problems:" section lists
completeness and quality issues (missing required fields, a field
disagreeing with the store, duplicate citation keys) and fires on
confirmed entries too - existing is not the same as being correct. A
malformed .bib entry is listed as skipped with its line number, and
the rest of the file is still audited.

The "Confirmed:" line maps each reference to its store key, with a
claims count ("2 claims recorded") when the matched entry carries
recorded semantic-verification claims. The -json output additionally
carries each matched entry's holdings (none/preprint/published/both),
which the prose report never prints.

options:
    -json         emit the report as JSON
    -online       re-verify store-held entries against their sources
                  too; a store-confirmed entry is never demoted by
                  online disagreement - the paper is held, so the
                  disagreement surfaces as a problem note alongside
                  the still-confirmed verdict
    -store <dir>  path to the paper store (overrides the configured store)
`

func init() {
	commands = append(commands, command{
		name: "audit",
		desc: "check a bibliography against the store and the sources",
		help: auditHelp,
		run:  runAudit,
	})
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
	entries, parseErrs := bibtex.Parse(f)
	for _, pe := range parseErrs {
		// Line 0 is a failure to read the file at all, not a bad entry;
		// there is nothing to audit past it.
		if pe.Line == 0 {
			return fmt.Errorf("audit: %s: %s", fs.Arg(0), pe.Msg)
		}
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
	for _, pe := range parseErrs {
		report.ParseErrors = append(report.ParseErrors, auditParseError{Line: pe.Line, Msg: pe.Msg})
	}

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
	// A duplicated citation key is a bibliography defect in its own
	// right, and the parser keeps both entries, so it is reported here:
	// on the later occurrence, naming where the first one was.
	firstLine := make(map[string]int, len(entries))
	for _, e := range entries {
		item := auditEntry{Key: e.Key, Line: e.Line, Existence: "unchecked"}
		if prev, seen := firstLine[e.Key]; seen {
			item.Problems = append(item.Problems,
				fmt.Sprintf("duplicate citation key (first used at line %d)", prev))
		} else {
			firstLine[e.Key] = e.Line
		}
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
			// A store match is itself evidence the paper exists — the store
			// holds it. Online silence can never outweigh that, so a
			// notFound verdict on a store-matched entry is clamped to
			// unverified, with the store key recorded as the counter-
			// evidence "likely hallucinated" would otherwise contradict.
			// (A draft match is never promoted all the way to confirmed —
			// only the "clean" case above does that.)
			if item.StoreKey != "" && item.Existence == "notFound" {
				item.Existence = "unverified"
				item.Problems = append(item.Problems, fmt.Sprintf(
					"held in the store as %s; no online source knows it", item.StoreKey))
			}
		case a.online:
			// Only a real online verdict is a disagreement worth a note.
			// "unchecked" means a source was down: nothing was learned,
			// so nothing is recorded.
			if verdict, cands := a.verify(e); verdict != "confirmed" && verdict != "unchecked" {
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
