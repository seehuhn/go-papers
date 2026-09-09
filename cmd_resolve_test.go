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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// crossrefPublishedSPDEResponse is the Crossref record for the published
// version of the preprint in arxivResponseWithDOI, so that resolving the
// arXiv ID reports the same DOI the preprint names.
const crossrefPublishedSPDEResponse = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1234/example.doi","type":"journal-article",
  "title":["A study of SPDEs in Greenland"],
  "container-title":["Journal of Cold Analysis"],
  "author":[{"given":"Jochen","family":"Vo\u00df","sequence":"first"}],
  "published":{"date-parts":[[2025,4]]}}}`

// crossrefBookResponse is a Crossref record of type "book" carrying no
// ISBN of its own, so that resolving it has to ask Open Library.
const crossrefBookResponse = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1000/widgets","type":"book",
  "title":["A Book About Widgets"],
  "author":[{"given":"Ann","family":"Author","sequence":"first"}],
  "publisher":"Acme","published":{"date-parts":[[1994]]}}}`

// crossrefBookWithISBNResponse is a Crossref "book" record that does
// carry an ISBN, for the case where Open Library knows the work not at
// all.
const crossrefBookWithISBNResponse = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1000/widgets","type":"book",
  "title":["A Book About Widgets"],
  "author":[{"given":"Ann","family":"Author","sequence":"first"}],
  "ISBN":["9780521406499"],
  "publisher":"Acme","published":{"date-parts":[[1994]]}}}`

// olResolveSearchFixture is an Open Library search response naming one
// work, whose title matches crossrefBookResponse exactly.
const olResolveSearchFixture = `{"docs":[
  {"key":"/works/OL123W","title":"A Book About Widgets",
   "author_name":["Ann Author"],"first_publish_year":1994}]}`

// olResolveEditionsFixture holds two editions of that work with distinct
// ISBNs, so that one is picked and the other reported as an alternative.
const olResolveEditionsFixture = `{"entries":[
  {"key":"/books/OL2M","title":"A Book About Widgets",
   "isbn_13":["9780521406499"],"publish_date":"1996",
   "physical_format":"Paperback","publishers":["Acme"]},
  {"key":"/books/OL1M","title":"A Book About Widgets",
   "isbn_13":["9780521006019"],"publish_date":"1994",
   "physical_format":"Hardcover","publishers":["Acme"]}]}`

// openLibraryServer serves the search and editions fixtures above at the
// paths the Open Library client uses.
func openLibraryServer(t *testing.T, search string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, search)
	})
	mux.HandleFunc("/works/OL123W/editions.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, olResolveEditionsFixture)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// crossrefServer serves one canned response for every request.
func crossrefServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// wantLine fails the test unless out holds the labelled line exactly,
// padding included.
func wantLine(t *testing.T, out, label, value string) {
	t.Helper()
	line := padLabel(label) + " " + value
	for _, got := range strings.Split(out, "\n") {
		if got == line {
			return
		}
	}
	t.Errorf("missing line %q in output:\n%s", line, out)
}

// padLabel renders a label the way resolve's output does, so that the
// tests check the alignment rather than restate it.
func padLabel(label string) string {
	s := label + ":"
	for len(s) < 8 {
		s += " "
	}
	return s
}

func TestResolveDOI(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, crossrefWorkResponse), "", "", "", "", "", "")

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"10.1080/01621459.1963.10500830"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "doi", "10.1080/01621459.1963.10500830")
	wantLine(t, out, "author", "Hoeffding, Wassily")
	wantLine(t, out, "year", "1963")
	wantLine(t, out, "type", "article")
	if !strings.Contains(out, "Probability Inequalities") {
		t.Errorf("output does not name the title:\n%s", out)
	}
	if strings.Contains(out, "isbn:") {
		t.Errorf("a journal article has no ISBN:\n%s", out)
	}
}

func TestResolveArxivWithDOI(t *testing.T) {
	initStore(t, "test@example.org")
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseWithDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	overrideBases(t, crossrefServer(t, crossrefPublishedSPDEResponse), arxivSrv.URL, "", "", "", "", "")

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"arXiv:2412.05039v2"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "doi", "10.1234/example.doi")
	wantLine(t, out, "arxiv", "2412.05039v2")
	wantLine(t, out, "year", "2025")
	wantLine(t, out, "type", "article")
}

func TestResolveFreeTextAmbiguous(t *testing.T) {
	initStore(t, "test@example.org")
	// Open Library also answers, with a work whose title is nowhere near
	// the query, so it is listed as a candidate but never accepted.
	olSearch := `{"docs":[{"key":"/works/OL9W","title":"Something Quite Different",
	  "author_name":["Cara Third"],"first_publish_year":1977}]}`
	overrideBases(t, crossrefServer(t, crossrefAmbiguousSearchResponse), "", "", "", "", "",
		openLibraryServer(t, olSearch))

	err := runResolve([]string{"Some", "Ambiguous", "Title"})
	if err == nil {
		t.Fatal("free text matching nothing clearly must fail")
	}
	msg := err.Error()
	for _, want := range []string{
		"cannot resolve",
		"[1] crossref:",
		"10.1000/first",
		"10.1000/second",
		"openlibrary:",
		"Something Quite Different",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message does not mention %q:\n%s", want, msg)
		}
	}
	// resolve takes no ISBN: sources.ParseRef has no ISBN kind, so an
	// ISBN typed back at it would be searched as free text and find
	// nothing. The re-run advice must not offer one.
	if strings.Contains(msg, "ISBN") {
		t.Errorf("the re-run advice offers an ISBN, which resolve cannot take:\n%s", msg)
	}
}

func TestResolveBookGetsISBNFromOpenLibrary(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, crossrefBookResponse), "", "", "", "", "",
		openLibraryServer(t, olResolveSearchFixture))

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"10.1000/widgets"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "doi", "10.1000/widgets")
	wantLine(t, out, "isbn", "9780521006019 (hardcover 1994)")
	wantLine(t, out, "also", "9780521406499 (paperback 1996)")
	wantLine(t, out, "type", "book")
}

func TestResolveBookFallsBackToCrossrefISBN(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, crossrefBookWithISBNResponse), "", "", "", "", "",
		openLibraryServer(t, `{"docs":[]}`))

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"10.1000/widgets"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "isbn", "9780521406499")
	if strings.Contains(out, "also:") {
		t.Errorf("a Crossref ISBN has no alternative editions:\n%s", out)
	}
}

func TestResolveFreeTextOnlyOpenLibraryMatches(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, `{"message":{"items":[]}}`), "", "", "", "", "",
		openLibraryServer(t, olResolveSearchFixture))

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"A", "Book", "About", "Widgets"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "isbn", "9780521006019 (hardcover 1994)")
	wantLine(t, out, "author", "Author, Ann")
	wantLine(t, out, "year", "1994")
	wantLine(t, out, "type", "book")
	if strings.Contains(out, "doi:") {
		t.Errorf("Open Library knows no DOI:\n%s", out)
	}
}

// olCitationSearchFixture is an Open Library search response for a book
// whose title is only part of the reference the caller typed.
const olCitationSearchFixture = `{"docs":[
  {"key":"/works/OL123W","title":"A Book About Widgets",
   "author_name":["Ann Author"],"first_publish_year":1996}]}`

// olTwoMatchesSearchFixture names two works that both fit the reference,
// so neither can be taken.
const olTwoMatchesSearchFixture = `{"docs":[
  {"key":"/works/OL123W","title":"A Book About Widgets",
   "author_name":["Ann Author"],"first_publish_year":1994},
  {"key":"/works/OL456W","title":"A Book About Widgets",
   "author_name":["Ann Author"],"first_publish_year":1996}]}`

// TestResolveFreeTextCitationShape covers the reference shape the help
// text documents, "Author, Title, year": the author and the year are
// extra words the work's title does not have, and they must not stop it
// from being recognised.
func TestResolveFreeTextCitationShape(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, `{"message":{"items":[]}}`), "", "", "", "", "",
		openLibraryServer(t, olCitationSearchFixture))

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"Author,", "A", "Book", "About", "Widgets,", "1996"})
	})
	if err != nil {
		t.Fatal(err)
	}

	// The reference names 1996, so the 1996 edition is the one picked.
	wantLine(t, out, "isbn", "9780521406499 (paperback 1996)")
	wantLine(t, out, "also", "9780521006019 (hardcover 1994)")
	wantLine(t, out, "type", "book")
}

// TestResolveFreeTextTwoOpenLibraryMatches is the other half of the
// uniqueness rule: two works fit the reference equally well, so resolve
// takes neither and lists both.
func TestResolveFreeTextTwoOpenLibraryMatches(t *testing.T) {
	initStore(t, "test@example.org")
	overrideBases(t, crossrefServer(t, `{"message":{"items":[]}}`), "", "", "", "", "",
		openLibraryServer(t, olTwoMatchesSearchFixture))

	err := runResolve([]string{"Author,", "A", "Book", "About", "Widgets,", "1996"})
	if err == nil {
		t.Fatal("two equally good works must not resolve to either")
	}
	msg := err.Error()
	if strings.Count(msg, "openlibrary:") != 2 {
		t.Errorf("error message should list both works:\n%s", msg)
	}
}

// TestResolveOpenLibraryDownIsANote pins the failure path: Open Library
// being unreachable must not be confused with it knowing no ISBN, and
// must not throw away the ISBN Crossref did supply.
func TestResolveOpenLibraryDownIsANote(t *testing.T) {
	initStore(t, "test@example.org")
	olSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(olSrv.Close)
	overrideBases(t, crossrefServer(t, crossrefBookWithISBNResponse), "", "", "", "", "", olSrv.URL)

	var err error
	out := captureStdout(t, func() {
		err = runResolve([]string{"10.1000/widgets"})
	})
	if err != nil {
		t.Fatal(err)
	}

	wantLine(t, out, "isbn", "9780521406499")
	if !strings.Contains(out, "note:") || !strings.Contains(out, "open library") {
		t.Errorf("a failed Open Library lookup must say so:\n%s", out)
	}
}
