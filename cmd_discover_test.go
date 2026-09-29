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

const discoverFixture = `{"meta": {"count": 57}, "results": [
 {"id": "https://openalex.org/W1", "doi": "https://doi.org/10.1080/ABC",
  "display_name": "Isotonic regression revisited", "publication_year": 2020,
  "type": "article", "cited_by_count": 12,
  "authorships": [{"author": {"display_name": "Jane Smith"}}],
  "primary_location": {"source": {"display_name": "Annals of Statistics"}}},
 {"id": "https://openalex.org/W2", "doi": null,
  "display_name": "Monotone estimation", "publication_year": 2019,
  "type": "article", "cited_by_count": 5,
  "authorships": [{"author": {"display_name": "Bo Chen"}}]}
]}`

func TestDiscover(t *testing.T) {
	_, dir := fixtureStore(t)
	var query string
	overrideOpenAlex(t, openAlexServer(t, discoverFixture, &query).URL)

	var err error
	out := captureStdout(t, func() {
		err = runDiscover([]string{"-n", "2", "isotonic", "regression"})
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "total: 57, shown: 2" {
		t.Errorf("first line %q", lines[0])
	}
	if got := len(lines); got != 5 { // header + 2 works + 2 abstract lines
		t.Errorf("got %d lines, want 5:\n%s", got, out)
	}
	if !strings.HasPrefix(lines[1], "W1 | 2020 | Smith | Isotonic regression revisited | Annals of Statistics | cited 12 | doi:10.1080/ABC") {
		t.Errorf("work line %q", lines[1])
	}

	q, _ := url.ParseQuery(query)
	if q.Get("search") != "isotonic regression" || q.Get("per_page") != "2" {
		t.Errorf("query %q", query)
	}

	log := readEventLog(t, dir)
	for _, want := range []string{`"command":"discover"`, `"source":"openalex"`, `"hits":2`, `"ref":"isotonic regression"`} {
		if !strings.Contains(log, want) {
			t.Errorf("event log lacks %s:\n%s", want, log)
		}
	}
}

func TestDiscoverMarksHeldPapers(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("smith_2020")
	p.DOI = "10.1080/abc"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	overrideOpenAlex(t, openAlexServer(t, discoverFixture, nil).URL)

	var err error
	out := captureStdout(t, func() {
		err = runDiscover([]string{"-short", "isotonic"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "held:smith_2020") {
		t.Errorf("held paper not marked:\n%s", out)
	}
}

func TestDiscoverSinceAndNoHits(t *testing.T) {
	_, dir := fixtureStore(t)
	var query string
	overrideOpenAlex(t, openAlexServer(t, `{"meta": {"count": 0}, "results": []}`, &query).URL)

	var err error
	out := captureStdout(t, func() {
		err = runDiscover([]string{"-since", "2015", "nothing"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "total: 0, shown: 0" {
		t.Errorf("output %q", out)
	}
	if !strings.Contains(query, "from_publication_date%3A2015-01-01") {
		t.Errorf("since filter missing: %q", query)
	}
	if log := readEventLog(t, dir); !strings.Contains(log, `"outcome":"no-hits"`) {
		t.Errorf("event log lacks no-hits outcome:\n%s", log)
	}
}

func TestDiscoverErrorIsLogged(t *testing.T) {
	_, dir := fixtureStore(t)
	overrideOpenAlex(t, openAlexServer(t, `not json`, nil).URL)

	var err error
	captureStdout(t, func() { err = runDiscover([]string{"x"}) })
	if err == nil || !strings.HasPrefix(err.Error(), "discover:") {
		t.Fatalf("error %v, want prefix discover:", err)
	}
	log := readEventLog(t, dir)
	if !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"detail"`) {
		t.Errorf("event log lacks error outcome and detail:\n%s", log)
	}
}

func TestDiscoverJSON(t *testing.T) {
	fixtureStore(t)
	overrideOpenAlex(t, openAlexServer(t, discoverFixture, nil).URL)

	var err error
	out := captureStdout(t, func() { err = runDiscover([]string{"-json", "isotonic"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"total": 57`) || !strings.Contains(out, `"id": "W1"`) {
		t.Errorf("JSON output:\n%s", out)
	}
}

func TestDiscoverNeedsQuery(t *testing.T) {
	fixtureStore(t)
	err := runDiscover(nil)
	if err == nil || !strings.HasPrefix(err.Error(), "discover:") {
		t.Fatalf("error %v, want prefix discover:", err)
	}
}

func TestTrailingFlagsGetAHint(t *testing.T) {
	fixtureStore(t)
	for _, c := range []struct {
		run  func([]string) error
		args []string
		want string
	}{
		{runDiscover, []string{"gaussian", "processes", "-since", "2020"},
			`discover: flags go before the query (got "-since")`},
		{runCiting, []string{"W1", "-since", "2020"},
			`citing: flags go before the ID (got "-since")`},
		{runRelated, []string{"refs.bib", "-n", "5"},
			`related: flags go before the .bib file (got "-n")`},
		{runRefs, []string{"W1", "-short"},
			`refs: flags go before the ID (got "-short")`},
	} {
		err := c.run(c.args)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: error %v, want %q", c.args, err, c.want)
		}
	}
}
