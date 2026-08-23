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
	"os"
	"sort"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{"bib", "export bibliography entries in BibTeX format", runBib})
}

// runBib implements the "paper bib" command: it loads one or more papers
// (or all papers with -all), and outputs them in BibTeX format to stdout,
// sorted by key, with blank lines between entries. Unknown keys produce
// an error suggesting "paper search".
func runBib(args []string) error {
	fs, storeFlag := newFlagSet("bib")
	allFlag := fs.Bool("all", false, "export all papers in the store")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("bib: parsing arguments: %w", err)
	}

	keys := fs.Args()

	// Check for conflicting or missing arguments
	if *allFlag && len(keys) > 0 {
		return fmt.Errorf("bib: cannot use -all with explicit keys; use one or the other")
	}
	if !*allFlag && len(keys) == 0 {
		return fmt.Errorf("bib: specify one or more keys, or use -all to export all papers")
	}

	s, _, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("bib: %w", err)
	}

	var papers []*store.Paper

	if *allFlag {
		// Load all papers
		papers, err = s.LoadAll()
		if err != nil {
			return fmt.Errorf("bib: %w", err)
		}
	} else {
		// Deduplicate keys while preserving order
		seen := make(map[string]bool)
		uniqueKeys := make([]string, 0, len(keys))
		for _, key := range keys {
			if !seen[key] {
				seen[key] = true
				uniqueKeys = append(uniqueKeys, key)
			}
		}

		// Load specified keys
		papers = make([]*store.Paper, 0, len(uniqueKeys))
		for _, key := range uniqueKeys {
			p, err := s.Load(key)
			if err != nil {
				// Check if this is a "not found" error (entry directory or paper.json doesn't exist)
				// vs. an actual load/parse error (corrupted JSON, permissions, etc.)
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("bib: paper %q not found; try 'paper search %s'", key, key)
				}
				// For other errors, report the underlying issue
				return fmt.Errorf("bib: cannot load paper %q: %w", key, err)
			}
			papers = append(papers, p)
		}
	}

	// Sort papers by key
	sort.Slice(papers, func(i, j int) bool {
		return papers[i].Key < papers[j].Key
	})

	// Output papers in BibTeX format with blank line separation
	for i, p := range papers {
		if i > 0 {
			fmt.Print("\n")
		}
		fmt.Print(bibtex.Format(p.Key, p.Bibtex))
	}

	return nil
}
