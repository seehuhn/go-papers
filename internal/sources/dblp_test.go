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

const dblpFixture = `{"result":{"hits":{"hit":[
  {"info":{"title":"Concentration Inequalities.","venue":"COLT","year":"2010",
    "doi":"10.1234/COLT.2010.99",
    "authors":{"author":[{"text":"Gabor Lugosi"},{"text":"Pascal Massart"}]}}}]}}}`

const dblpNoHitsFixture = `{"result":{"hits":{}}}`

const dblpSingleAuthorFixture = `{"result":{"hits":{"hit":[
  {"info":{"title":"Solo-Authored Paper.","venue":"CoRR","year":"2015",
    "authors":{"author":{"text":"Solo Author"}}}}]}}}`

const dblpVenueArrayFixture = `{"result":{"hits":{"hit":[
  {"info":{"title":"Cross-Listed Paper.","venue":["COLT","ALT"],"year":"2012",
    "authors":{"author":[{"text":"Author One"}]}}}]}}}`

const dblpTwoHitBrokenFixture = `{"result":{"hits":{"hit":[
  {"info":{"title":12345,"venue":"Bogus","year":"2020"}},
  {"info":{"title":"Concentration Inequalities.","venue":"COLT","year":"2010",
    "doi":"10.1234/COLT.2010.99",
    "authors":{"author":[{"text":"Gabor Lugosi"},{"text":"Pascal Massart"}]}}}]}}}`

func TestDBLPSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("format param missing, url %s", r.URL)
		}
		io.WriteString(w, dblpFixture)
	}))
	t.Cleanup(srv.Close)
	d := &DBLP{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := d.Search("concentration inequalities", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Source != "dblp" || hits[0].Year != 2010 ||
		len(hits[0].Authors) != 2 || hits[0].Authors[0] != "Gabor Lugosi" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestDBLPSearchSingleAuthorObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, dblpSingleAuthorFixture)
	}))
	t.Cleanup(srv.Close)
	d := &DBLP{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := d.Search("solo authored paper", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || len(hits[0].Authors) != 1 || hits[0].Authors[0] != "Solo Author" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestDBLPSearchVenueArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, dblpVenueArrayFixture)
	}))
	t.Cleanup(srv.Close)
	d := &DBLP{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := d.Search("cross listed paper", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Venue != "COLT" {
		t.Errorf("hits = %+v, want Venue = COLT", hits)
	}
}

func TestDBLPSearchSkipsMalformedHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, dblpTwoHitBrokenFixture)
	}))
	t.Cleanup(srv.Close)
	d := &DBLP{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := d.Search("concentration inequalities", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DOI != "10.1234/COLT.2010.99" {
		t.Errorf("hits = %+v, want exactly the well-formed hit", hits)
	}
}

func TestDBLPSearchNoHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, dblpNoHitsFixture)
	}))
	t.Cleanup(srv.Close)
	d := &DBLP{BaseURL: srv.URL, Client: srv.Client()}
	hits, err := d.Search("no such publication", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want empty slice", hits)
	}
}
