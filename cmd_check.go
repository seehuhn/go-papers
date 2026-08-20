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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{"check", "validate entries and store invariants", runCheck})
}

// entryLoad is the result of loading one entry by its directory name: the
// name itself, the parsed paper (nil on failure), and any load error. It
// is the unit of work shared between runCheck's per-entry CheckPaper pass
// and fsck's store-wide invariant pass, so that each entry's paper.json is
// read from disk exactly once per "paper check" run.
type entryLoad struct {
	dirKey string
	paper  *store.Paper
	err    error
}

// runCheck implements the "paper check" command: it validates one or
// more store entries (or, if no keys are given, every entry in the
// store plus the store-wide invariants checked by fsck), prints one
// line per problem found, and promotes any draft entry that turns out
// to be clean. It returns a non-nil error if any error-severity problem
// was found.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	storeFlag := fs.String("store", "", "path to the paper store (overrides PAPER_STORE)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("paper check: parsing arguments: %w", err)
	}

	s, err := store.Open(*storeFlag)
	if err != nil {
		return fmt.Errorf("paper check: %w", err)
	}

	keys := fs.Args()
	wholeStore := len(keys) == 0
	if wholeStore {
		allKeys, err := s.Keys()
		if err != nil {
			return fmt.Errorf("paper check: %w", err)
		}
		keys = allKeys
	}

	// Load every selected entry exactly once. The load results feed both
	// the per-entry CheckPaper pass below and, for a whole-store run,
	// fsck's store-wide invariant checks - so a corrupt paper.json is
	// reported once, not once per pass.
	loads := make([]entryLoad, 0, len(keys))
	for _, key := range keys {
		p, err := s.Load(key)
		loads = append(loads, entryLoad{dirKey: key, paper: p, err: err})
	}

	// entryResult tracks, for one successfully loaded entry, the paper
	// plus the count of error-severity problems found for it so far, so
	// that draft promotion below can see the combined result of both the
	// CheckPaper pass and (for whole-store runs) the fsck pass.
	type entryResult struct {
		dirKey string
		paper  *store.Paper
		errors int
	}

	var problems []store.Problem
	var results []*entryResult
	byDir := make(map[string]*entryResult)

	for _, l := range loads {
		if l.err != nil {
			problems = append(problems, store.Problem{Key: l.dirKey, Severity: "error",
				Msg: fmt.Sprintf("cannot load entry: %v", l.err)})
			continue
		}
		r := &entryResult{dirKey: l.dirKey, paper: l.paper}

		// Directory name / paper.json key mismatch. This is checked here,
		// in the per-entry loop, rather than only in fsck's whole-store
		// pass, so that "paper check <dir>" (explicit-key mode) catches it
		// too: promoting a mismatched draft would make s.Save write to
		// s.Dir(p.Key) - the stale body key - creating a new directory
		// while the old one keeps the draft file behind.
		if l.paper.Key != l.dirKey {
			problems = append(problems, store.Problem{Key: l.dirKey, Severity: "error",
				Msg: fmt.Sprintf("directory name %q does not match paper.json key %q", l.dirKey, l.paper.Key)})
			r.errors++
		}

		for _, prob := range store.CheckPaper(l.paper) {
			// CheckPaper reports problems under Paper.Key, the key
			// recorded inside paper.json. Rewrite it to the directory
			// name so that, even when the two disagree (a mismatch
			// checked above), every problem for one physical entry is
			// printed and tallied under the same, dereferenceable
			// identity.
			prob.Key = l.dirKey
			problems = append(problems, prob)
			if prob.Severity == "error" {
				r.errors++
			}
		}
		results = append(results, r)
		byDir[l.dirKey] = r
	}

	if wholeStore {
		for _, prob := range fsck(s, loads) {
			problems = append(problems, prob)
			if prob.Severity == "error" {
				if r, ok := byDir[prob.Key]; ok {
					r.errors++
				}
			}
		}
	}

	errorCount := 0
	for _, prob := range problems {
		fmt.Printf("%s: %s: %s\n", prob.Key, prob.Severity, prob.Msg)
		if prob.Severity == "error" {
			errorCount++
		}
	}

	// Draft promotion: any entry that is still marked "draft", has no
	// pending task, and has zero error-severity problems is ready to be
	// promoted to "clean". This can happen on any check run that covers
	// the entry, whether by explicit key or as part of a whole-store
	// fsck.
	for _, r := range results {
		p := r.paper
		if p.Status != "draft" || p.Pending != "" || r.errors > 0 {
			continue
		}
		p.Status = "clean"
		if err := s.Save(p); err != nil {
			return fmt.Errorf("paper check: promoting %q to clean: %w", r.dirKey, err)
		}
		fmt.Printf("%s: promoted from draft to clean\n", r.dirKey)
	}

	if errorCount > 0 {
		return fmt.Errorf("paper check: found %d error(s) among %d problem(s)", errorCount, len(problems))
	}
	return nil
}

// fsck checks store-wide invariants that CheckPaper cannot see because it
// only looks at a single Paper value: Versions entries with no file on
// disk, files in an entry directory that are not listed in Versions, and
// stray sync-conflict files anywhere in the store. (The directory-name/key
// mismatch check that used to live here has moved to runCheck's per-entry
// loop, so it also fires in explicit-key mode.) loads is the set of entries
// already read from disk by the caller (runCheck); fsck reuses that data
// instead of reloading each paper.json itself, so a load failure is
// reported exactly once overall.
func fsck(s *store.Store, loads []entryLoad) []store.Problem {
	var problems []store.Problem

	for _, l := range loads {
		if l.err != nil {
			// Already reported once by the caller's own load pass.
			continue
		}
		key := l.dirKey
		p := l.paper

		// The directory-name/key mismatch check now lives in runCheck's
		// per-entry loop (above), so that it fires in explicit-key mode
		// too, not just here in the whole-store pass.

		dir := s.Dir(key)

		// Files listed in Versions but missing on disk. Versions filenames
		// are sorted first so output order is stable across runs (map
		// iteration order is not).
		filenames := make([]string, 0, len(p.Versions))
		for filename := range p.Versions {
			filenames = append(filenames, filename)
		}
		sort.Strings(filenames)
		for _, filename := range filenames {
			path := filepath.Join(dir, filename)
			if _, err := os.Stat(path); err != nil {
				problems = append(problems, store.Problem{Key: key, Severity: "error",
					Msg: fmt.Sprintf("versions: %q is listed but missing on disk", filename)})
			}
		}

		// Files present in the entry directory but not listed in
		// Versions (paper.json itself is exempt).
		entries, err := os.ReadDir(dir)
		if err != nil {
			problems = append(problems, store.Problem{Key: key, Severity: "error",
				Msg: fmt.Sprintf("reading entry directory: %v", err)})
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if name == "paper.json" {
				continue
			}
			if _, ok := p.Versions[name]; ok {
				continue
			}
			if strings.Contains(name, "sync-conflict") {
				// Reported separately below, by the whole-store walk.
				continue
			}
			kind := "file"
			if e.IsDir() {
				kind = "directory"
			}
			problems = append(problems, store.Problem{Key: key, Severity: "error",
				Msg: fmt.Sprintf("%s %q is present but not listed in versions", kind, name)})
		}
	}

	// Sync-conflict files anywhere in the store.
	filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.Contains(d.Name(), "sync-conflict") {
			rel, relErr := filepath.Rel(s.Root, path)
			if relErr != nil {
				rel = path
			}
			key := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			problems = append(problems, store.Problem{Key: key, Severity: "error",
				Msg: fmt.Sprintf("sync-conflict file found: %s", rel)})
		}
		return nil
	})

	return problems
}
