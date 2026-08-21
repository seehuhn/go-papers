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

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Root: t.TempDir()}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := testStore(t)
	p := &Paper{
		Key:    "voss_2004",
		Status: "clean",
		Bibtex: bibtex.Entry{Type: "phdthesis", Fields: map[string]string{
			"author": `Vo{\ss}, Jochen`,
			"title":  "Some large deviation results for diffusion processes",
			"school": "Universit{\"a}t Kaiserslautern",
			"year":   "2004",
		}},
		Holdings: "none",
	}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("voss_2004")
	if err != nil {
		t.Fatal(err)
	}
	if got.Bibtex.Fields["author"] != `Vo{\ss}, Jochen` {
		t.Errorf("author round-trip: %q", got.Bibtex.Fields["author"])
	}
	if got.Holdings != "none" {
		t.Errorf("holdings = %q", got.Holdings)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	s := testStore(t)
	p := &Paper{Key: "a_2000", Status: "draft", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "misc", Fields: map[string]string{}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root, "a_2000"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestKeysSkipsJunk(t *testing.T) {
	s := testStore(t)
	os.MkdirAll(filepath.Join(s.Root, "inbox"), 0o755) // no paper.json
	os.WriteFile(filepath.Join(s.Root, "config.json"), []byte("{}"), 0o644)
	p := &Paper{Key: "b_2001", Status: "draft", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "misc", Fields: map[string]string{}}}
	s.Save(p)
	keys, err := s.Keys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "b_2001" {
		t.Errorf("Keys() = %v, want [b_2001]", keys)
	}
}

// TestLoadRejectsUnknownMembers covers an agent typo (e.g. "absract"
// instead of "abstract") in paper.json. Before this fix, json.Unmarshal
// silently dropped the unrecognized member, and check's draft promotion
// would then rewrite paper.json from the in-memory struct, physically
// deleting the typo'd data. Load must instead fail, so the problem
// surfaces (as "paper check" reporting "cannot load entry ...") instead of
// silently destroying data.
func TestLoadRejectsUnknownMembers(t *testing.T) {
	s := testStore(t)
	dir := filepath.Join(s.Root, "voss_2004")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{
  "key": "voss_2004",
  "status": "clean",
  "holdings": "none",
  "absract": "a typo'd top-level member",
  "bibtex": {"type": "misc", "fields": {}}
}
`
	if err := os.WriteFile(filepath.Join(dir, "paper.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := s.Load("voss_2004")
	if err == nil {
		t.Fatal("expected an error for an unknown top-level member")
	}
	if !strings.Contains(err.Error(), "absract") {
		t.Errorf("error should mention the unknown member %q: %v", "absract", err)
	}
}

func TestOpenResolution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PAPER_STORE", dir)
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != dir {
		t.Errorf("Root = %q, want %q", s.Root, dir)
	}
	if _, err := Open(filepath.Join(dir, "missing")); err == nil {
		t.Error("expected error for missing store dir")
	}
}

func TestOpenUnconfigured(t *testing.T) {
	t.Setenv("PAPER_STORE", "")
	_, err := Open("")
	if err == nil {
		t.Fatal("expected error when no store is configured")
	}
	if !strings.Contains(err.Error(), "PAPER_STORE") {
		t.Errorf("error %q should mention PAPER_STORE", err)
	}
}
