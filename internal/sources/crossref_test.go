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

package sources

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const crossrefWorkFixture = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1080/01621459.1963.10500830","type":"journal-article",
  "title":["Probability Inequalities for Sums of Bounded Random Variables"],
  "container-title":["Journal of the American Statistical Association"],
  "author":[{"given":"Wassily","family":"Hoeffding","sequence":"first"}],
  "volume":"58","issue":"301","page":"13-30",
  "ISSN":["0162-1459"],"publisher":"Informa UK Limited",
  "published":{"date-parts":[[1963,3]]}}}`

const crossrefSearchFixture = `{"status":"ok","message-type":"work-list","message":{"items":[
  {"DOI":"10.1080/01621459.1963.10500830","type":"journal-article","score":95.5,
   "title":["Probability Inequalities for Sums of Bounded Random Variables"],
   "author":[{"given":"Wassily","family":"Hoeffding"}],
   "published":{"date-parts":[[1963]]}},
  {"DOI":"10.2307/2282952","type":"journal-article","score":40.1,
   "title":["Something Else Entirely"],
   "published":{"date-parts":[[1964]]}}]}}`

func newCrossrefTestServer(t *testing.T) (*Crossref, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/works/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1080/01621459.1963.10500830" {
			io.WriteString(w, crossrefWorkFixture)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/works", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query.bibliographic"); got == "" {
			t.Errorf("search request missing query.bibliographic, url %s", r.URL)
		}
		if got := r.URL.Query().Get("mailto"); got != "test@example.org" {
			t.Errorf("mailto = %q, want test@example.org", got)
		}
		io.WriteString(w, crossrefSearchFixture)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Crossref{BaseURL: srv.URL, Client: srv.Client(), Email: "test@example.org"}, srv
}

func TestCrossrefWork(t *testing.T) {
	c, _ := newCrossrefTestServer(t)
	w, err := c.Work("10.1080/01621459.1963.10500830")
	if err != nil {
		t.Fatal(err)
	}
	if w.Titles[0] != "Probability Inequalities for Sums of Bounded Random Variables" {
		t.Errorf("title = %q", w.Titles)
	}
	if w.Authors[0].Family != "Hoeffding" || w.Authors[0].Given != "Wassily" {
		t.Errorf("author = %+v", w.Authors)
	}
	if w.Published.Year() != 1963 {
		t.Errorf("year = %d, want 1963", w.Published.Year())
	}
	if w.Page != "13-30" || w.Volume != "58" || w.Issue != "301" {
		t.Errorf("pages/volume/issue = %q/%q/%q", w.Page, w.Volume, w.Issue)
	}
}

// crossrefOrgAuthorFixture is a mixed author array, as seen on the LIGO
// gravitational-wave discovery paper (10.1103/PhysRevLett.116.061102):
// ordinary {family,given} entries alongside a collective-author entry
// that carries only a literal "name".
const crossrefOrgAuthorFixture = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1103/PhysRevLett.116.061102","type":"journal-article",
  "title":["Observation of Gravitational Waves"],
  "container-title":["Physical Review Letters"],
  "author":[{"given":"B. P.","family":"Abbott"},
            {"name":"LIGO Scientific Collaboration"}],
  "volume":"116","issue":"6","page":"061102",
  "published":{"date-parts":[[2016,2]]}}}`

func TestCrossrefWorkOrgAuthor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/works/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefOrgAuthorFixture)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Crossref{BaseURL: srv.URL, Client: srv.Client()}

	w, err := c.Work("10.1103/PhysRevLett.116.061102")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Authors) != 2 {
		t.Fatalf("authors = %+v, want 2 entries", w.Authors)
	}
	if w.Authors[0].Family != "Abbott" || w.Authors[0].Given != "B. P." || w.Authors[0].Name != "" {
		t.Errorf("author[0] = %+v", w.Authors[0])
	}
	if w.Authors[1].Name != "LIGO Scientific Collaboration" || w.Authors[1].Family != "" || w.Authors[1].Given != "" {
		t.Errorf("author[1] = %+v", w.Authors[1])
	}
}

func TestCrossrefWorkNotFound(t *testing.T) {
	c, _ := newCrossrefTestServer(t)
	_, err := c.Work("10.9999/nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing DOI: got %v, want a 'not found' error", err)
	}
}

func TestCrossrefSearch(t *testing.T) {
	c, _ := newCrossrefTestServer(t)
	hits, err := c.Search("Hoeffding probability inequalities 1963", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Score <= hits[1].Score {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("top hit DOI = %q", hits[0].DOI)
	}
}
