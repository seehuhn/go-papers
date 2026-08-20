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
	"os"
	"path/filepath"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func fixtureStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PAPER_STORE", dir)
	return &store.Store{Root: dir}, dir
}

func cleanPaper(key string) *store.Paper {
	return &store.Paper{Key: key, Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963",
			"doi":     "10.1080/01621459.1963.10500830",
		}}}
}

func TestCheckCleanStore(t *testing.T) {
	s, _ := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	if err := runCheck(nil); err != nil {
		t.Errorf("clean store: %v", err)
	}
}

func TestCheckFindsKeyMismatch(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	s.Save(p)
	// dirname/key mismatch: rename the directory
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))
	if err := runCheck(nil); err == nil {
		t.Error("expected error for key/dirname mismatch")
	}
}

func TestCheckFindsSyncConflict(t *testing.T) {
	s, dir := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	os.WriteFile(filepath.Join(dir, "hoeffding_1963",
		"paper.sync-conflict-20260820-XYZ.json"), []byte("{}"), 0o644)
	if err := runCheck(nil); err == nil {
		t.Error("expected error for sync-conflict file")
	}
}

func TestCheckPromotesDraft(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.Status = "draft"
	s.Save(p)
	if err := runCheck(nil); err != nil {
		t.Fatalf("check: %v", err)
	}
	got, _ := s.Load("hoeffding_1963")
	if got.Status != "clean" {
		t.Errorf("status = %q, want clean", got.Status)
	}
}

func TestCheckUnknownKeyArg(t *testing.T) {
	fixtureStore(t)
	if err := runCheck([]string{"nope_1900"}); err == nil {
		t.Error("expected error for unknown key")
	}
}
