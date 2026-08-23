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
	"net/http"
	"net/http/httptest"
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

func TestSearchCandidateOrderIsDeterministicOnTies(t *testing.T) {
	// Three sources each return a hit with an identical title, so all
	// three candidates score identically; the reported order must still
	// be the same on every run.
	title := "The Exact Same Title"
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"message":{"items":[{"DOI":"10.1/c","title":[%q],"issued":{"date-parts":[[2001]]}}]}}`, title)
	}))
	t.Cleanup(crossrefSrv.Close)
	zbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"result":[{"title":{"title":%q},"year":2001}]}`, title)
	}))
	t.Cleanup(zbSrv.Close)
	dblpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"result":{"hits":{"hit":[{"info":{"title":%q,"year":"2001"}}]}}}`, title)
	}))
	t.Cleanup(dblpSrv.Close)
	overrideBases(t, crossrefSrv.URL, refusingServer(t), refusingServer(t), zbSrv.URL, dblpSrv.URL)

	a := &auditor{api: crossrefSrv.Client()}
	e := bibtex.KeyedEntry{Entry: bibtex.Entry{Type: "misc",
		Fields: map[string]string{"title": "A Different Title Entirely"}}}

	first, ok := a.search(e)
	if !ok || len(first) != 3 {
		t.Fatalf("search = %v, %v; want 3 candidates", first, ok)
	}
	// Equal scores break the tie on the source name, so the order is a
	// defined property, not an accident of the sort: alphabetical.
	want := []string{"crossref", "dblp", "zbmath"}
	for j, w := range want {
		if first[j].Source != w {
			t.Fatalf("tied candidates in order %v, want sources %v",
				[]string{first[0].Source, first[1].Source, first[2].Source}, want)
		}
	}
}

func TestSearchCandidateTiebreakerIsTotalOnSourceAndScore(t *testing.T) {
	// Two candidates from the same source with identical score and no DOI
	// must still have a defined order. zbmath returns two hits in reverse
	// alphabetical title order; the defined tiebreak order must be
	// alphabetical by title (and authors if titles match).
	queryTitle := "Something"
	zbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server returns hits in reverse alphabetical order: "Zulu" then "Alpha".
		// If the comparator has only score/source/DOI tiebreaks, they stay reversed.
		// With title/authors tiebreaks, they should sort alphabetically.
		fmt.Fprintf(w, `{"result":[`+
			`{"title":{"title":"Zulu"},"year":2001},`+
			`{"title":{"title":"Alpha"},"year":2001}`+
			`]}`)
	}))
	t.Cleanup(zbSrv.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, emptyList.URL, refusingServer(t), refusingServer(t), zbSrv.URL, emptyList.URL)

	a := &auditor{api: zbSrv.Client()}
	e := bibtex.KeyedEntry{Entry: bibtex.Entry{Type: "misc",
		Fields: map[string]string{"title": queryTitle}}}

	got, ok := a.search(e)
	if !ok || len(got) != 2 {
		t.Fatalf("search = %v, %v; want 2 candidates", got, ok)
	}
	// Both have identical score and source, empty DOI, but different titles.
	// The defined order must be alphabetical by title: Alpha before Zulu.
	if got[0].Title != "Alpha" || got[1].Title != "Zulu" {
		t.Errorf("candidates in order %v, want [Alpha, Zulu] (alphabetical by title)",
			[]string{got[0].Title, got[1].Title})
	}
}

func TestArxivIDOfPreservesOldStyleCase(t *testing.T) {
	e := bibtex.Entry{Fields: map[string]string{"eprint": "arXiv:math.PR/0605234"}}

	if got := arxivIDOf(e); got != "math.PR/0605234" {
		t.Errorf("arxivIDOf = %q, want %q (the archive class is case-sensitive)", got, "math.PR/0605234")
	}
}
