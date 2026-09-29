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
	"net/url"
	"strings"
	"testing"
)

const citingList = `{"meta": {"count": 900}, "results": [
 {"id": "https://openalex.org/W9", "display_name": "Newest", "publication_year": 2026,
  "type": "article", "cited_by_count": 0,
  "authorships": [{"author": {"display_name": "Jane Smith"}}]}
]}`

func TestCitingNewestFirstWithTotal(t *testing.T) {
	_, dir := fixtureStore(t)
	rec := openAlexRoutes(t, map[string]string{
		"/works/doi:10.1234/top": `{"id": "https://openalex.org/W100", "display_name": "T"}`,
		"/works":                 citingList,
	})

	var err error
	out := captureStdout(t, func() { err = runCiting([]string{"-since", "2020", "-short", "10.1234/top"}) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "total: 900, shown: 1" {
		t.Errorf("header %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "W9 | 2026 | Smith | Newest") {
		t.Errorf("work line %q", lines[1])
	}

	got := rec.paths()
	if len(got) != 2 {
		t.Fatalf("requests %v", got)
	}
	q, _ := url.ParseQuery(strings.SplitN(got[1], "?", 2)[1])
	if q.Get("sort") != "publication_date:desc" || q.Get("per_page") != "50" {
		t.Errorf("query %v", q)
	}
	if f := q.Get("filter"); f != "cites:W100,from_publication_date:2020-01-01" {
		t.Errorf("filter %q", f)
	}
	log := readEventLog(t, dir)
	for _, want := range []string{`"command":"citing"`, `"outcome":"ok"`, `"hits":1`, `"ref":"10.1234/top"`} {
		if !strings.Contains(log, want) {
			t.Errorf("event log lacks %s:\n%s", want, log)
		}
	}
}

func TestCitingSkipsLookupForOpenAlexID(t *testing.T) {
	fixtureStore(t)
	rec := openAlexRoutes(t, map[string]string{"/works": citingList})

	var err error
	captureStdout(t, func() { err = runCiting([]string{"W1"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rec.paths() {
		if strings.HasPrefix(r, "/works/W1") {
			t.Errorf("unexpected lookup request %q", r)
		}
	}
	if len(rec.paths()) != 1 {
		t.Errorf("requests %v, want exactly the citing query", rec.paths())
	}
}

func TestCitingUnknownLogsMiss(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{})

	var err error
	captureStdout(t, func() { err = runCiting([]string{"10.9999/none"}) })
	if err == nil || !strings.HasPrefix(err.Error(), "citing: OpenAlex does not know 10.9999/none") {
		t.Fatalf("error %v", err)
	}
	if log := readEventLog(t, dir); !strings.Contains(log, `"outcome":"openalex-unknown"`) {
		t.Errorf("event log lacks openalex-unknown:\n%s", log)
	}
}

func TestCitingNoHits(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{"/works": `{"meta": {"count": 0}, "results": []}`})

	var err error
	out := captureStdout(t, func() { err = runCiting([]string{"W1"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "total: 0, shown: 0" {
		t.Errorf("output %q", out)
	}
	if log := readEventLog(t, dir); !strings.Contains(log, `"outcome":"no-hits"`) {
		t.Errorf("event log lacks no-hits:\n%s", log)
	}
}

func TestCitingErrorIsLogged(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{"/works": `not json`})

	var err error
	captureStdout(t, func() { err = runCiting([]string{"W1"}) })
	if err == nil || !strings.HasPrefix(err.Error(), "citing:") {
		t.Fatalf("error %v, want prefix citing:", err)
	}
	log := readEventLog(t, dir)
	if !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"detail"`) {
		t.Errorf("event log lacks error outcome and detail:\n%s", log)
	}
}

func TestCitingNeedsOneID(t *testing.T) {
	fixtureStore(t)
	for _, args := range [][]string{nil, {"a", "b"}} {
		err := runCiting(args)
		if err == nil || !strings.HasPrefix(err.Error(), "citing:") {
			t.Errorf("args %v: error %v, want prefix citing:", args, err)
		}
	}
}
