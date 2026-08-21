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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seehuhn.de/go/paper/internal/bibtex"
)

func attachFixture(t *testing.T) (*Store, *Paper) {
	t.Helper()
	s := testStore(t)
	p := &Paper{Key: "hoeffding_1963", Status: "draft", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "JASA", "year": "1963"}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestAttachMovesAndRecords(t *testing.T) {
	s, p := attachFixture(t)
	src := filepath.Join(t.TempDir(), "download.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	if err := s.Attach(p, src, "published.pdf", "unpaywall", now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Error("source file should have been moved away")
	}
	if _, err := os.Stat(filepath.Join(s.Dir(p.Key), "published.pdf")); err != nil {
		t.Error("file should exist in the paper directory")
	}
	if p.Holdings != "published" {
		t.Errorf("holdings = %q, want published", p.Holdings)
	}
	v := p.Versions["published.pdf"]
	if v.Acquired != "2026-08-21" || v.Source != "unpaywall" {
		t.Errorf("version record = %+v", v)
	}
	if len(p.Log) != 1 || p.Log[0].Action != "attach" {
		t.Errorf("log = %+v, want one attach entry", p.Log)
	}
	// reload from disk: Attach must have Saved
	q, err := s.Load(p.Key)
	if err != nil || q.Holdings != "published" {
		t.Errorf("reload: (%+v, %v)", q, err)
	}
}

func TestAttachRefusesOverwrite(t *testing.T) {
	s, p := attachFixture(t)
	dst := filepath.Join(s.Dir(p.Key), "published.pdf")
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "download.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := s.Attach(p, src, "published.pdf", "x", time.Now())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("overwrite must be refused with a clear error, got %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("source must be left in place when Attach fails")
	}
}

func TestRecomputeHoldings(t *testing.T) {
	cases := []struct {
		files []string
		want  string
	}{
		{nil, "none"},
		{[]string{"arxiv-2412.05039v2.pdf"}, "preprint"},
		{[]string{"published.pdf"}, "published"},
		{[]string{"published.pdf", "arxiv-2412.05039v2.pdf"}, "both"},
	}
	for _, c := range cases {
		p := &Paper{Versions: map[string]Version{}}
		for _, f := range c.files {
			p.Versions[f] = Version{}
		}
		RecomputeHoldings(p)
		if p.Holdings != c.want {
			t.Errorf("files %v: holdings = %q, want %q", c.files, p.Holdings, c.want)
		}
	}
}
