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
	"time"

	"seehuhn.de/go/paper/internal/resolve"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

// newFlagSet returns the standard flag set for a paper subcommand: named
// after the command, ContinueOnError semantics, and the shared -store
// flag. The returned string pointer holds the -store value after Parse.
func newFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	storeFlag := fs.String("store", "", "path to the paper store (overrides PAPER_STORE)")
	return fs, storeFlag
}

// findDuplicate returns the key of the entry that already holds the given
// DOI or arXiv ID, or "" when the store has none. Either identifier may be
// empty, in which case it is not looked for.
func findDuplicate(s *store.Store, doi, arxivID string) (string, error) {
	papers, err := s.LoadAll()
	if err != nil {
		return "", err
	}
	for _, p := range papers {
		if doi != "" && strings.EqualFold(p.DOI, doi) {
			return p.Key, nil
		}
		if arxivID != "" && p.Arxiv != nil && p.Arxiv.ID == arxivID {
			return p.Key, nil
		}
	}
	return "", nil
}

// createDraft picks a free key for a freshly resolved draft entry, records
// under the given log action why it exists, and writes it to the store.
func createDraft(s *store.Store, p *store.Paper, now time.Time, action, detail string) error {
	key, err := s.FreeKey(p.Key)
	if err != nil {
		return err
	}
	p.Key = key
	p.AppendLog(now, action, detail)
	if err := s.Save(p); err != nil {
		return err
	}
	fmt.Printf("created %s\n", p.Key)
	return nil
}

// attachFile hands srcPath to store.Attach, which moves it into the paper
// directory and records it. On a failed Attach the in-memory paper is
// stale — mutated but not saved — so it is discarded and reloaded from the
// store, and the reloaded paper is returned instead.
//
// The returned paper is never nil, even when that reload fails too:
// callers build their hand-off message from it, and its identity fields
// (key, bibtex) survive a failed Attach untouched. Only a paper returned
// alongside a nil error is safe to save.
func attachFile(s *store.Store, p *store.Paper, srcPath, filename, source string, now time.Time) (*store.Paper, error) {
	if err := s.Attach(p, srcPath, filename, source, now); err != nil {
		reloaded, loadErr := s.Load(p.Key)
		if loadErr != nil {
			return p, fmt.Errorf("%w (reloading %s afterwards also failed: %v)", err, p.Key, loadErr)
		}
		return reloaded, err
	}
	return p, nil
}

// outcomeError tags an error, returned from one of a command's outcome
// branches, with the event-log outcome it corresponds to ("no-oa-route",
// "ambiguous", "duplicate", "unidentified", "mismatch", ...), so that a
// deferred logger at the top of the command can classify the run without
// inspecting message text. It wraps the original error, which Unwrap
// still exposes.
type outcomeError struct {
	outcome string
	err     error
}

func (e *outcomeError) Error() string { return e.err.Error() }
func (e *outcomeError) Unwrap() error { return e.err }

// wrapOutcome tags err with the given event-log outcome.
func wrapOutcome(outcome string, err error) error {
	return &outcomeError{outcome: outcome, err: err}
}

// eventOutcome classifies err for the event log: "ok" for a nil error,
// the tagged outcome for an error made with wrapOutcome, and "error" for
// anything else.
func eventOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	var oe *outcomeError
	if errors.As(err, &oe) {
		return oe.outcome
	}
	return "error"
}

// mergePublished overlays the published Crossref record named by an arXiv
// preprint onto the preprint's draft entry: the published metadata is the
// entry, per the spec's best-version rule. A Crossref failure is not
// fatal — the arXiv-only entry is still useful — but it must be visible in
// Pending, so the preprint draft is returned with a note instead.
func mergePublished(c *sources.Crossref, p *store.Paper, entry *sources.ArxivEntry, doi string) *store.Paper {
	work, err := c.Work(doi)
	if err != nil {
		p.Pending = addPending(p.Pending, fmt.Sprintf(
			"crossref lookup of %s failed (%v); published metadata is missing", doi, err))
		return p
	}
	published, err := resolve.FromCrossref(work)
	if err != nil {
		p.Pending = addPending(p.Pending, fmt.Sprintf(
			"crossref record %s is unusable (%v); published metadata is missing", doi, err))
		return p
	}
	return resolve.Merge(published, entry)
}
