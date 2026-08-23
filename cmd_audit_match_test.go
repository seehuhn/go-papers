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
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func TestMatchStoreByDOI(t *testing.T) {
	papers := []*store.Paper{
		{Key: "hoeffding_1963", DOI: "10.1080/01621459.1963.10500830"},
		{Key: "other_2001", DOI: "10.1000/other"},
	}
	e := bibtex.KeyedEntry{Key: "hoef63", Entry: bibtex.Entry{Type: "article",
		Fields: map[string]string{"doi": "10.1080/01621459.1963.10500830"}}}

	got := matchStore(papers, e)

	if got == nil || got.Key != "hoeffding_1963" {
		t.Errorf("matchStore = %v, want hoeffding_1963", got)
	}
}

func TestMatchStoreIsCaseInsensitiveOnDOI(t *testing.T) {
	papers := []*store.Paper{{Key: "k", DOI: "10.1000/AbC"}}
	e := bibtex.KeyedEntry{Entry: bibtex.Entry{Fields: map[string]string{"doi": "10.1000/abc"}}}

	if got := matchStore(papers, e); got == nil {
		t.Error("DOIs are case-insensitive; want a match")
	}
}

func TestMatchStoreByTitleNeedsCorroboration(t *testing.T) {
	papers := []*store.Paper{{Key: "giles_2008", Bibtex: bibtex.Entry{Fields: map[string]string{
		"author": "Giles, Michael B.", "title": "Multilevel Monte Carlo path simulation", "year": "2008"}}}}
	// The pile ingest filed this wrong entry against that one: 5/6 Jaccard.
	e := bibtex.KeyedEntry{Entry: bibtex.Entry{Fields: map[string]string{
		"author": "Giles, Michael B. and Waterhouse, Benjamin",
		"title":  "Multilevel quasi-Monte Carlo path simulation",
		"year":   "2009"}}}

	if got := matchStore(papers, e); got != nil {
		t.Errorf("matchStore = %s; a near title with a different year must not match", got.Key)
	}
}

func TestDiffAgainstStoreReportsDisagreement(t *testing.T) {
	p := &store.Paper{Key: "hoeffding_1963", Status: "clean",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "J. Amer. Statist. Assoc.", "year": "1963",
			// A blank store field (Finding 8): must never be reported as
			// "missing field \"note\" (store has \"\")" — an empty store
			// value is nothing to be missing.
			"note": ""}}}
	e := bibtex.KeyedEntry{Key: "hoef", Entry: bibtex.Entry{Type: "article",
		Fields: map[string]string{
			"author": "Hoeffding, W.", "title": "Probability inequalities",
			"year": "1962"}}}

	got := diffAgainstStore(e, p)

	joined := strings.Join(got, "\n")
	for _, want := range []string{"year", "1963", "journal", "doi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff should mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"note"`) {
		t.Errorf("a blank store field must not be reported as missing:\n%s", joined)
	}
}

// TestDiffAgainstStoreOrderIsDeterministic pins Finding 6: diffAgainstStore
// must not iterate a Go map directly into its output, or the order of the
// reported problems varies from run to run. It uses a store entry with
// enough disagreeing fields — both bibtex.FieldOrder fields and an unknown
// one — that map-iteration nondeterminism would show up within a handful
// of runs.
func TestDiffAgainstStoreOrderIsDeterministic(t *testing.T) {
	p := &store.Paper{Key: "many", Status: "clean",
		DOI: "10.1000/many",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "A, One", "title": "T", "journal": "J", "year": "2001",
			"volume": "1", "number": "2", "pages": "1--10", "publisher": "Pub",
			"note": "N", "mrnumber": "12345",
		}}}
	e := bibtex.KeyedEntry{Key: "many", Entry: bibtex.Entry{Type: "article",
		Fields: map[string]string{"title": "T"}}}

	first := diffAgainstStore(e, p)
	for i := 0; i < 10; i++ {
		got := diffAgainstStore(e, p)
		if strings.Join(got, "\n") != strings.Join(first, "\n") {
			t.Fatalf("diffAgainstStore order is nondeterministic:\nrun 0: %v\nrun %d: %v", first, i+1, got)
		}
	}
}

func TestDiffAgainstStoreIsQuietWhenTheyAgree(t *testing.T) {
	fields := map[string]string{"author": "Hoeffding, Wassily",
		"title": "Probability inequalities", "journal": "JASA", "year": "1963"}
	p := &store.Paper{Key: "k", Status: "clean", Bibtex: bibtex.Entry{Type: "article", Fields: fields}}
	e := bibtex.KeyedEntry{Entry: bibtex.Entry{Type: "article", Fields: fields}}

	if got := diffAgainstStore(e, p); len(got) != 0 {
		t.Errorf("identical entries should diff clean, got %v", got)
	}
}
