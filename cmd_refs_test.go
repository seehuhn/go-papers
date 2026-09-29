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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// openAlexRecorder collects every request a test server receives, as
// "path?rawquery".
type openAlexRecorder struct {
	mu   sync.Mutex
	reqs []string
}

func (r *openAlexRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, s)
}

func (r *openAlexRecorder) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

// openAlexRoutes starts a server that answers each request path with the
// body registered for it, and with 404 for any other path. It points
// openAlexBase at the server for the rest of the test.
func openAlexRoutes(t *testing.T, routes map[string]string) *openAlexRecorder {
	t.Helper()
	rec := &openAlexRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path + "?" + r.URL.RawQuery)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	overrideOpenAlex(t, srv.URL)
	return rec
}

const refsWork = `{"id": "https://openalex.org/W100", "doi": "https://doi.org/10.1/top",
 "display_name": "The survey", "publication_year": 2021, "type": "article",
 "cited_by_count": 7,
 "authorships": [{"author": {"display_name": "Ada Lovelace"}}],
 "primary_location": {"source": {"display_name": "Survey Journal"}},
 "abstract_inverted_index": {"Short": [0], "abstract": [1]},
 "referenced_works": ["https://openalex.org/W3", "https://openalex.org/W1", "https://openalex.org/W2"]}`

// refsList lists the references in a different order than the work does.
const refsList = `{"meta": {"count": 3}, "results": [
 {"id": "https://openalex.org/W1", "doi": "https://doi.org/10.1080/ABC",
  "display_name": "First", "publication_year": 2000, "type": "article", "cited_by_count": 10,
  "authorships": [{"author": {"display_name": "Jane Smith"}}]},
 {"id": "https://openalex.org/W2", "display_name": "Second", "publication_year": 2001,
  "type": "article", "cited_by_count": 20,
  "authorships": [{"author": {"display_name": "Bo Chen"}}]},
 {"id": "https://openalex.org/W3", "display_name": "Third", "publication_year": 2002,
  "type": "article", "cited_by_count": 30,
  "authorships": [{"author": {"display_name": "Cy Young"}}]}
]}`

func TestRefsListsReferences(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("smith_2000")
	p.DOI = "10.1080/abc"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	rec := openAlexRoutes(t, map[string]string{"/works/W100": refsWork, "/works": refsList})

	var err error
	out := captureStdout(t, func() { err = runRefs([]string{"-short", "W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 { // work, header, three references
		t.Fatalf("got %d lines, want 5:\n%s", len(lines), out)
	}
	if lines[1] != "total: 3, shown: 3" {
		t.Errorf("header %q", lines[1])
	}
	for i, want := range []string{"W3 | 2002 | Young", "W1 | 2000 | Smith", "W2 | 2001 | Chen"} {
		if !strings.HasPrefix(lines[2+i], want) {
			t.Errorf("line %d = %q, want prefix %q", 2+i, lines[2+i], want)
		}
	}
	if !strings.Contains(lines[3], "held:smith_2000") {
		t.Errorf("held reference not marked: %q", lines[3])
	}
	if strings.Contains(lines[2], "held:") || strings.Contains(lines[4], "held:") {
		t.Errorf("unheld references marked:\n%s", out)
	}
	if got := rec.paths(); len(got) != 2 {
		t.Fatalf("requests %v, want a work lookup and one batch", got)
	}
	q, _ := url.ParseQuery(strings.SplitN(rec.paths()[1], "?", 2)[1])
	if q.Get("filter") != "openalex_id:W3|W1|W2" {
		t.Errorf("batch filter %q", q.Get("filter"))
	}
}

func TestRefsPrintsResolvedWorkFirst(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("lovelace_2021")
	p.DOI = "10.1/TOP"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	openAlexRoutes(t, map[string]string{"/works/W100": refsWork, "/works": refsList})

	var err error
	out := captureStdout(t, func() { err = runRefs([]string{"W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	want := "work: W100 | 2021 | Lovelace | The survey | Survey Journal | cited 7 | doi:10.1/top | held:lovelace_2021"
	if lines[0] != want {
		t.Errorf("first line %q, want %q", lines[0], want)
	}
	if lines[1] != "    abstract: Short abstract" {
		t.Errorf("second line %q, want the work's abstract", lines[1])
	}
	if lines[2] != "total: 3, shown: 3" {
		t.Errorf("third line %q", lines[2])
	}

	out = captureStdout(t, func() { err = runRefs([]string{"-json", "W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"work": {`, `"id": "W100"`, `"held": "lovelace_2021"`, `"total": 3`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output lacks %s:\n%s", want, out)
		}
	}
	if strings.Index(out, `"work"`) > strings.Index(out, `"works"`) {
		t.Errorf("work member should come first:\n%s", out)
	}
}

func TestRefsUnknownLogsMiss(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{})

	var err error
	captureStdout(t, func() { err = runRefs([]string{"W999"}) })
	if err == nil || !strings.HasPrefix(err.Error(), "refs: OpenAlex does not know W999") {
		t.Fatalf("error %v, want \"refs: OpenAlex does not know W999\"", err)
	}
	log := readEventLog(t, dir)
	for _, want := range []string{`"command":"refs"`, `"outcome":"openalex-unknown"`, `"ref":"W999"`, `"source":"openalex"`} {
		if !strings.Contains(log, want) {
			t.Errorf("event log lacks %s:\n%s", want, log)
		}
	}
}

func TestRefsNoRefsLogsMiss(t *testing.T) {
	for _, tc := range []struct{ typ, outcome string }{
		{"article", "openalex-no-refs"},
		{"book", "no-hits"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			_, dir := fixtureStore(t)
			body := `{"id": "https://openalex.org/W5", "display_name": "Bare", "publication_year": 2010,
 "type": "` + tc.typ + `", "cited_by_count": 1, "referenced_works": []}`
			rec := openAlexRoutes(t, map[string]string{"/works/W5": body})

			var err error
			out := captureStdout(t, func() { err = runRefs([]string{"W5"}) })
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "total: 0, shown: 0\n") {
				t.Errorf("output lacks empty header:\n%s", out)
			}
			note := strings.Contains(out, "OpenAlex lists no references for this article.")
			if note != (tc.typ == "article") {
				t.Errorf("note present = %v for type %s:\n%s", note, tc.typ, out)
			}
			if got := rec.paths(); len(got) != 1 {
				t.Errorf("requests %v, want only the work lookup", got)
			}
			if log := readEventLog(t, dir); !strings.Contains(log, `"outcome":"`+tc.outcome+`"`) {
				t.Errorf("event log lacks outcome %s:\n%s", tc.outcome, log)
			}
		})
	}
}

func TestRefsOKLogsHits(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{"/works/W100": refsWork, "/works": refsList})
	var err error
	captureStdout(t, func() { err = runRefs([]string{"W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	log := readEventLog(t, dir)
	for _, want := range []string{`"outcome":"ok"`, `"hits":3`, `"ref":"W100"`} {
		if !strings.Contains(log, want) {
			t.Errorf("event log lacks %s:\n%s", want, log)
		}
	}
}

func TestRefsNeedsOneID(t *testing.T) {
	fixtureStore(t)
	for _, args := range [][]string{nil, {"a", "b"}} {
		err := runRefs(args)
		if err == nil || !strings.HasPrefix(err.Error(), "refs:") {
			t.Errorf("args %v: error %v, want prefix refs:", args, err)
		}
	}
}
