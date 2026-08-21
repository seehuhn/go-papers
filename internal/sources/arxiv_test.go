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

const arxivFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2412.05039v2</id>
    <title>A study of SPDEs in Greenland</title>
    <summary>We study stochastic partial differential equations.</summary>
    <published>2024-12-06T14:00:00Z</published>
    <author><name>Jochen Voß</name></author>
    <author><name>Andrew M. Stuart</name></author>
    <arxiv:doi xmlns:arxiv="http://arxiv.org/schemas/atom">10.1234/example.doi</arxiv:doi>
    <arxiv:journal_ref xmlns:arxiv="http://arxiv.org/schemas/atom">J. Example 12 (2025) 1-20</arxiv:journal_ref>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="math.PR"/>
    <category term="math.PR"/>
  </entry>
</feed>`

const arxivEmptyFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"></feed>`

// arxivOldStyleFixture has an <id> with a category prefix, as used by
// pre-2007 arXiv IDs, and a version suffix.
const arxivOldStyleFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/math.PR/0605234v1</id>
    <title>An older paper</title>
    <summary>An older abstract.</summary>
    <published>2006-05-09T00:00:00Z</published>
    <author><name>Jochen Voß</name></author>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="math.PR"/>
  </entry>
</feed>`

// arxivNoVersionFixture has an <id> with no trailing version suffix.
const arxivNoVersionFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2412.05039</id>
    <title>A study of SPDEs in Greenland</title>
    <summary>We study stochastic partial differential equations.</summary>
    <published>2024-12-06T14:00:00Z</published>
    <author><name>Jochen Voß</name></author>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="math.PR"/>
  </entry>
</feed>`

func newArxivTestServer(t *testing.T, body string) *Arxiv {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/query" {
			t.Errorf("path = %q, want /api/query", r.URL.Path)
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Arxiv{BaseURL: srv.URL, Client: srv.Client()}
}

func TestArxivByID(t *testing.T) {
	a := newArxivTestServer(t, arxivFixture)
	e, err := a.ByID("2412.05039")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "2412.05039" || e.Version != 2 {
		t.Errorf("id/version = %q/%d, want 2412.05039/2", e.ID, e.Version)
	}
	if e.Title != "A study of SPDEs in Greenland" {
		t.Errorf("title = %q", e.Title)
	}
	if len(e.Authors) != 2 || e.Authors[0] != "Jochen Voß" {
		t.Errorf("authors = %v", e.Authors)
	}
	if e.DOI != "10.1234/example.doi" || e.Year != 2024 || e.PrimaryClass != "math.PR" {
		t.Errorf("doi/year/class = %q/%d/%q", e.DOI, e.Year, e.PrimaryClass)
	}
}

func TestArxivByIDOldStyle(t *testing.T) {
	a := newArxivTestServer(t, arxivOldStyleFixture)
	e, err := a.ByID("math.PR/0605234")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "math.PR/0605234" || e.Version != 1 {
		t.Errorf("id/version = %q/%d, want math.PR/0605234/1", e.ID, e.Version)
	}
}

func TestArxivByIDNoVersion(t *testing.T) {
	a := newArxivTestServer(t, arxivNoVersionFixture)
	e, err := a.ByID("2412.05039")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "2412.05039" || e.Version != 0 {
		t.Errorf("id/version = %q/%d, want 2412.05039/0", e.ID, e.Version)
	}
}

func TestArxivByIDNotFound(t *testing.T) {
	a := newArxivTestServer(t, arxivEmptyFixture)
	_, err := a.ByID("9999.99999")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("got %v, want a 'not found' error", err)
	}
}

func TestArxivURLs(t *testing.T) {
	if got := PDFURL("https://arxiv.org", "2412.05039", 2); got != "https://arxiv.org/pdf/2412.05039v2" {
		t.Errorf("PDFURL = %q", got)
	}
	if got := SourceURL("https://arxiv.org", "2412.05039", 2); got != "https://arxiv.org/e-print/2412.05039v2" {
		t.Errorf("SourceURL = %q", got)
	}
}
