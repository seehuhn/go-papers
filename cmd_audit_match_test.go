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
			"journal": "J. Amer. Statist. Assoc.", "year": "1963"}}}
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
