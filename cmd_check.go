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
	"strings"

	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{"check", "validate entries and store invariants", runCheck})
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

	// entryResult tracks, for one entry, the paper as loaded plus the
	// count of error-severity problems found for it so far. Errors are
	// tracked per directory name (rather than per Paper.Key) so that a
	// directory-name/key mismatch — itself an error reported by fsck —
	// reliably blocks draft promotion for that entry, even though the
	// mismatch problem and the entry's own paper.json disagree about
	// what its key is.
	type entryResult struct {
		dirKey string
		paper  *store.Paper
		errors int
	}

	var problems []store.Problem
	var results []*entryResult
	byDir := make(map[string]*entryResult)

	loadAndCheck := func(dirKey string) {
		p, err := s.Load(dirKey)
		if err != nil {
			problems = append(problems, store.Problem{Key: dirKey, Severity: "error",
				Msg: fmt.Sprintf("cannot load entry: %v", err)})
			return
		}
		r := &entryResult{dirKey: dirKey, paper: p}
		for _, prob := range store.CheckPaper(p) {
			problems = append(problems, prob)
			if prob.Severity == "error" {
				r.errors++
			}
		}
		results = append(results, r)
		byDir[dirKey] = r
	}

	if len(keys) > 0 {
		for _, key := range keys {
			loadAndCheck(key)
		}
	} else {
		allKeys, err := s.Keys()
		if err != nil {
			return fmt.Errorf("paper check: %w", err)
		}
		for _, key := range allKeys {
			loadAndCheck(key)
		}
		for _, prob := range fsck(s) {
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

// fsck checks store-wide invariants that CheckPaper cannot see because
// it only looks at a single Paper value: directory-name/key mismatches,
// Versions entries with no file on disk, files in an entry directory
// that are not listed in Versions, and stray sync-conflict files
// anywhere in the store.
func fsck(s *store.Store) []store.Problem {
	var problems []store.Problem

	keys, err := s.Keys()
	if err != nil {
		return []store.Problem{{Key: "<store>", Severity: "error",
			Msg: fmt.Sprintf("listing store: %v", err)}}
	}

	for _, key := range keys {
		p, err := s.Load(key)
		if err != nil {
			problems = append(problems, store.Problem{Key: key, Severity: "error",
				Msg: fmt.Sprintf("cannot load entry: %v", err)})
			continue
		}

		if p.Key != key {
			problems = append(problems, store.Problem{Key: key, Severity: "error",
				Msg: fmt.Sprintf("directory name %q does not match paper.json key %q", key, p.Key)})
		}

		dir := s.Dir(key)

		// Files listed in Versions but missing on disk.
		for filename := range p.Versions {
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
