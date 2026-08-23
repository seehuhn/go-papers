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

func TestHandleExistsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/handles/10.5281/zenodo.1234567" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		io.WriteString(w, `{"responseCode":1,"handle":"10.5281/zenodo.1234567","values":[]}`)
	}))
	t.Cleanup(srv.Close)
	h := &Handle{BaseURL: srv.URL, Client: srv.Client()}

	got, err := h.Exists("10.5281/zenodo.1234567")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Errorf("Exists = false, want true for responseCode 1")
	}
}

func TestHandleExistsFalseOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	h := &Handle{BaseURL: srv.URL, Client: srv.Client()}

	got, err := h.Exists("10.9999/nonexistent")
	if err != nil {
		t.Fatalf("a 404 is a definitive no, not an error: %v", err)
	}
	if got {
		t.Errorf("Exists = true, want false for a 404")
	}
}

func TestHandleExistsFalseOnResponseCode100(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"responseCode":100,"handle":"10.9999/nonexistent"}`)
	}))
	t.Cleanup(srv.Close)
	h := &Handle{BaseURL: srv.URL, Client: srv.Client()}

	got, err := h.Exists("10.9999/nonexistent")
	if err != nil {
		t.Fatalf("responseCode 100 is a definitive no, not an error: %v", err)
	}
	if got {
		t.Errorf("Exists = true, want false for responseCode 100")
	}
}

func TestHandleExistsErrorOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "handle system is down")
	}))
	t.Cleanup(srv.Close)
	h := &Handle{BaseURL: srv.URL, Client: srv.Client()}

	got, err := h.Exists("10.1000/whatever")
	if err == nil {
		t.Fatal("a 500 must be reported as an error, not treated as a definitive no")
	}
	if got {
		t.Errorf("Exists = true on error, want false")
	}
}

func TestHandleExistsErrorOnUnparsableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "not json at all")
	}))
	t.Cleanup(srv.Close)
	h := &Handle{BaseURL: srv.URL, Client: srv.Client()}

	got, err := h.Exists("10.1000/whatever")
	if err == nil {
		t.Fatal("an unparsable body must be reported as an error")
	}
	if got {
		t.Errorf("Exists = true on error, want false")
	}
}

func TestHandleDefaultBaseURL(t *testing.T) {
	h := &Handle{}
	if got := h.baseURL(); got != "https://doi.org" {
		t.Errorf("baseURL() = %q, want https://doi.org", got)
	}
}
