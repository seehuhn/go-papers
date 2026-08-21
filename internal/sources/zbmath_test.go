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
	"testing"
)

const zbmathFixture = `{"result":[
  {"title":{"title":"Probability inequalities for sums of bounded random variables"},
   "contributors":{"authors":[{"name":"Hoeffding, Wassily"}]},
   "source":{"series":[{"title":"J. Am. Stat. Assoc."}]},
   "year":1963,
   "links":[{"type":"doi","identifier":"10.1080/01621459.1963.10500830"}]}]}`

const zbmathTwoHitFixture = `{"result":[
  {"title":{"title":"Malformed Year Entry"},
   "year":"nineteen63",
   "links":[{"type":"doi","identifier":"10.9999/bad"}]},
  {"title":{"title":"Probability inequalities for sums of bounded random variables"},
   "contributors":{"authors":[{"name":"Hoeffding, Wassily"}]},
   "source":{"series":[{"title":"J. Am. Stat. Assoc."}]},
   "year":1963,
   "links":[{"type":"doi","identifier":"10.1080/01621459.1963.10500830"}]}]}`

func TestZbMathSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search_string") == "" {
			t.Errorf("missing search_string, url %s", r.URL)
		}
		io.WriteString(w, zbmathFixture)
	}))
	t.Cleanup(srv.Close)
	z := &ZbMath{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := z.Search("Hoeffding inequalities", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Source != "zbmath" ||
		hits[0].DOI != "10.1080/01621459.1963.10500830" || hits[0].Year != 1963 {
		t.Errorf("hits = %+v", hits)
	}
}

func TestZbMathSearchSkipsMalformedHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, zbmathTwoHitFixture)
	}))
	t.Cleanup(srv.Close)
	z := &ZbMath{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := z.Search("Hoeffding inequalities", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 ||
		hits[0].Title != "Probability inequalities for sums of bounded random variables" ||
		hits[0].DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("hits = %+v, want exactly the well-formed hit", hits)
	}
}
