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

const unpaywallFixture = `{"doi":"10.1234/example.doi","is_oa":true,
  "best_oa_location":{"url_for_pdf":"https://repo.example.org/paper.pdf",
    "license":"cc-by","host_type":"repository"}}`

func TestUnpaywallLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("email") != "test@example.org" {
			t.Errorf("email param missing, url %s", r.URL)
		}
		io.WriteString(w, unpaywallFixture)
	}))
	t.Cleanup(srv.Close)
	u := &Unpaywall{BaseURL: srv.URL, Client: srv.Client(), Email: "test@example.org"}
	res, err := u.Lookup("10.1234/example.doi")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsOA || res.BestOALocation == nil ||
		res.BestOALocation.PDFURL != "https://repo.example.org/paper.pdf" {
		t.Errorf("result = %+v", res)
	}
}

func TestUnpaywallRequiresEmail(t *testing.T) {
	u := &Unpaywall{BaseURL: "http://invalid.invalid"}
	_, err := u.Lookup("10.1/x")
	if err == nil || !strings.Contains(err.Error(), "config.json") {
		t.Errorf("missing email: got %v, want an error mentioning config.json", err)
	}
}

func TestUnpaywallTaggedEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("email") != "test+tag@example.org" {
			t.Errorf("email param not properly encoded, url %s, got %s", r.URL, r.URL.Query().Get("email"))
		}
		io.WriteString(w, unpaywallFixture)
	}))
	t.Cleanup(srv.Close)
	u := &Unpaywall{BaseURL: srv.URL, Client: srv.Client(), Email: "test+tag@example.org"}
	res, err := u.Lookup("10.1234/example.doi")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsOA || res.BestOALocation == nil {
		t.Errorf("result = %+v", res)
	}
}
