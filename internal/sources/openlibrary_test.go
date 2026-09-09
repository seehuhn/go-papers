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

const olSearchFixture = `{"docs":[
  {"key":"/works/OL123W","title":"A Book About Widgets",
   "author_name":["Ann Author"],"first_publish_year":1994}]}`

func newOpenLibraryTestServer(t *testing.T) (*OpenLibrary, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("title"); got == "" {
			t.Errorf("search request missing title, url %s", r.URL)
		}
		io.WriteString(w, olSearchFixture)
	})
	mux.HandleFunc("/works/OL123W/editions.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, olEditionsFixture)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &OpenLibrary{BaseURL: srv.URL, Client: srv.Client()}, srv
}

func TestOpenLibrarySearch(t *testing.T) {
	o, _ := newOpenLibraryTestServer(t)
	works, err := o.Search("Widgets", "Ann Author", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 {
		t.Fatalf("works = %+v, want 1 entry", works)
	}
	w := works[0]
	if w.Key != "/works/OL123W" {
		t.Errorf("key = %q", w.Key)
	}
	if w.Title != "A Book About Widgets" {
		t.Errorf("title = %q", w.Title)
	}
	if len(w.Authors) != 1 || w.Authors[0] != "Ann Author" {
		t.Errorf("authors = %+v", w.Authors)
	}
	if w.FirstYear != 1994 {
		t.Errorf("first year = %d, want 1994", w.FirstYear)
	}
}

func TestOpenLibrarySearchNoAuthor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("author"); got != "" {
			t.Errorf("author = %q, want omitted", got)
		}
		io.WriteString(w, olSearchFixture)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	o := &OpenLibrary{BaseURL: srv.URL, Client: srv.Client()}

	if _, err := o.Search("Widgets", "", 5); err != nil {
		t.Fatal(err)
	}
}

// olEditionsFixture carries editions with isbn_13, isbn_10-only (converted),
// and an unparsable ISBN, exercising the ISBN-preference and
// publish_date-year-parsing rules.
const olEditionsFixture = `{"entries":[
  {"key":"/books/OL1M","title":"A Book About Widgets",
   "isbn_13":["9780521006019"],"publish_date":"1994",
   "physical_format":"Hardcover","edition_name":"2nd ed.","publishers":["Acme"]},
  {"key":"/books/OL2M","title":"A Book About Widgets",
   "isbn_10":["0521006015"],"publish_date":"March 21, 1995",
   "physical_format":"Paperback","publishers":["Acme"]},
  {"key":"/books/OL3M","title":"A Book About Widgets",
   "publish_date":"1996"}]}`

func TestOpenLibraryEditions(t *testing.T) {
	o, _ := newOpenLibraryTestServer(t)
	editions, err := o.Editions("/works/OL123W")
	if err != nil {
		t.Fatal(err)
	}
	if len(editions) != 3 {
		t.Fatalf("editions = %+v, want 3 entries", editions)
	}

	e0 := editions[0]
	if e0.ISBN != "9780521006019" {
		t.Errorf("e0 ISBN = %q, want isbn_13 preferred", e0.ISBN)
	}
	if e0.Year != 1994 {
		t.Errorf("e0 Year = %d, want 1994", e0.Year)
	}
	if e0.Format != "hardcover" {
		t.Errorf("e0 Format = %q, want lower-cased", e0.Format)
	}
	if e0.EditionName != "2nd ed." {
		t.Errorf("e0 EditionName = %q", e0.EditionName)
	}

	e1 := editions[1]
	if e1.ISBN != "9780521006019" {
		t.Errorf("e1 ISBN = %q, want isbn_10 converted to isbn-13", e1.ISBN)
	}
	if e1.Year != 1995 {
		t.Errorf("e1 Year = %d, want 1995 parsed from last 4-digit run", e1.Year)
	}

	e2 := editions[2]
	if e2.ISBN != "" {
		t.Errorf("e2 ISBN = %q, want empty (no isbn)", e2.ISBN)
	}
	if e2.Format != "" {
		t.Errorf("e2 Format = %q, want empty (unknown)", e2.Format)
	}
}

func mkEdition(isbn string, year int, format, editionName string) OLEdition {
	return OLEdition{Key: "/books/" + isbn, Title: "t", ISBN: isbn, Year: year, Format: format, EditionName: editionName}
}

func TestPickEdition(t *testing.T) {
	tests := []struct {
		name       string
		editions   []OLEdition
		edition    string
		year       int
		wantISBN   string
		wantOthers []string // others' ISBNs, in the expected ranked order
		wantOK     bool
	}{
		{
			name:     "no editions",
			editions: nil,
			wantOK:   false,
		},
		{
			name: "only editions without ISBN do not qualify",
			editions: []OLEdition{
				{Key: "/books/OL1M", ISBN: ""},
			},
			wantOK: false,
		},
		{
			name: "single qualifying edition",
			editions: []OLEdition{
				mkEdition("9780000000002", 2000, "hardcover", ""),
			},
			wantISBN:   "9780000000002",
			wantOthers: []string{},
			wantOK:     true,
		},
		{
			name: "edition-name ordinal digit tie-break",
			editions: []OLEdition{
				mkEdition("9780000000011", 2000, "paperback", "1st ed."),
				mkEdition("9780000000022", 2000, "paperback", "2nd ed."),
			},
			edition:    "2nd",
			year:       2000,
			wantISBN:   "9780000000022",
			wantOthers: []string{"9780000000011"},
			wantOK:     true,
		},
		{
			name: "year tie-break",
			editions: []OLEdition{
				mkEdition("9780000000011", 1999, "paperback", ""),
				mkEdition("9780000000022", 2005, "paperback", ""),
			},
			year:       2005,
			wantISBN:   "9780000000022",
			wantOthers: []string{"9780000000011"},
			wantOK:     true,
		},
		{
			name: "format tie-break: hardcover beats paperback beats other beats e-book",
			editions: []OLEdition{
				mkEdition("9780000000011", 2000, "electronic resource", ""),
				mkEdition("9780000000022", 2000, "", ""),
				mkEdition("9780000000033", 2000, "paperback", ""),
				mkEdition("9780000000044", 2000, "hardcover", ""),
			},
			wantISBN:   "9780000000044",
			wantOthers: []string{"9780000000033", "9780000000022", "9780000000011"},
			wantOK:     true,
		},
		{
			name: "format tie-break: other/unknown beats e-book variants",
			editions: []OLEdition{
				mkEdition("9780000000011", 2000, "ebook", ""),
				mkEdition("9780000000022", 2000, "kindle", ""),
				mkEdition("9780000000033", 2000, "microform", ""),
			},
			wantISBN:   "9780000000033",
			wantOthers: []string{"9780000000011", "9780000000022"},
			wantOK:     true,
		},
		{
			name: "earliest year tie-break",
			editions: []OLEdition{
				mkEdition("9780000000011", 2010, "hardcover", ""),
				mkEdition("9780000000022", 1990, "hardcover", ""),
				mkEdition("9780000000033", 2000, "hardcover", ""),
			},
			wantISBN:   "9780000000022",
			wantOthers: []string{"9780000000033", "9780000000011"},
			wantOK:     true,
		},
		{
			name: "smallest ISBN tie-break",
			editions: []OLEdition{
				mkEdition("9780000000033", 2000, "hardcover", ""),
				mkEdition("9780000000011", 2000, "hardcover", ""),
				mkEdition("9780000000022", 2000, "hardcover", ""),
			},
			wantISBN:   "9780000000011",
			wantOthers: []string{"9780000000022", "9780000000033"},
			wantOK:     true,
		},
		{
			name: "unknown edition/year are no-ops, format still decides",
			editions: []OLEdition{
				mkEdition("9780000000011", 1980, "paperback", "3rd ed."),
				mkEdition("9780000000022", 2020, "hardcover", "1st ed."),
			},
			wantISBN:   "9780000000022",
			wantOthers: []string{"9780000000011"},
			wantOK:     true,
		},
		{
			// Input order deliberately differs from ranked order, and a
			// no-ISBN edition sits between qualifying ones: others must
			// come back ranked (next-best first), not in leftover input
			// order, and the no-ISBN edition must not appear at all.
			name: "others follow ranked order and exclude no-ISBN editions",
			editions: []OLEdition{
				mkEdition("9780000000020", 2000, "paperback", ""),
				{Key: "/books/OLxM", ISBN: "", Format: "hardcover", Year: 1970},
				mkEdition("9780000000030", 2005, "hardcover", ""),
				mkEdition("9780000000010", 1990, "hardcover", ""),
			},
			wantISBN:   "9780000000010",
			wantOthers: []string{"9780000000030", "9780000000020"},
			wantOK:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pick, others, ok := PickEdition(tc.editions, tc.edition, tc.year)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if pick.ISBN != tc.wantISBN {
				t.Errorf("pick.ISBN = %q, want %q", pick.ISBN, tc.wantISBN)
			}
			gotOthers := make([]string, len(others))
			for i, o := range others {
				gotOthers[i] = o.ISBN
			}
			if len(gotOthers) != len(tc.wantOthers) {
				t.Fatalf("others ISBNs = %v, want %v", gotOthers, tc.wantOthers)
			}
			for i := range gotOthers {
				if gotOthers[i] != tc.wantOthers[i] {
					t.Errorf("others ISBNs = %v, want %v", gotOthers, tc.wantOthers)
					break
				}
			}
		})
	}
}
