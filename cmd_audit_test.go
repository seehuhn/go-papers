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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func TestAuditOfflineMatchesTheStore(t *testing.T) {
	dir := initStore(t, "test@example.org")
	guardBases(t)
	s := openConfiguredStore(t)
	p := &store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "J. Amer. Statist. Assoc.", "year": "1963"}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	bib := filepath.Join(dir, "refs.bib")
	if err := os.WriteFile(bib, []byte(`@article{hoef,
  author = {Hoeffding, Wassily},
  title = {Probability inequalities},
  year = {1963},
  doi = {10.1080/01621459.1963.10500830},
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runAudit([]string{bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	if !strings.Contains(out, "hoeffding_1963") {
		t.Errorf("output should name the matched store entry:\n%s", out)
	}
	if !strings.Contains(out, "journal") {
		t.Errorf("the bib entry is missing the journal the store has; audit should say so:\n%s", out)
	}
}

func TestAuditNeverWritesToTheStore(t *testing.T) {
	dir := initStore(t, "")
	guardBases(t)
	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte("@article{k,\n  title = {Nothing},\n  year = {1999},\n}"), 0o644)
	before := storeSnapshot(t, dir)

	captureStdout(t, func() { runAudit([]string{bib}) })

	if after := storeSnapshot(t, dir); after != before {
		t.Errorf("audit is read-only but the store changed:\nbefore %v\nafter  %v", before, after)
	}
}

// storeSnapshot returns a stable description of every file under dir, so a
// test can assert that a command left the store untouched.
func storeSnapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %d\n", path, info.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
