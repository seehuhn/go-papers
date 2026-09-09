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

package store

import (
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
)

// exportTestPaper returns a book entry with neither a bibtex.fields.doi
// nor a bibtex.fields.isbn, so tests can vary only the top-level DOI and
// ISBN.
func exportTestPaper() *Paper {
	return &Paper{
		Key:    "voss_2004",
		Status: "clean",
		Bibtex: bibtex.Entry{Type: "book", Fields: map[string]string{
			"author":    "Voss, Jochen",
			"title":     "Some Book",
			"publisher": "Some Publisher",
			"year":      "2004",
		}},
	}
}

func TestExportBibtexAddsTopLevelIdentifiers(t *testing.T) {
	p := exportTestPaper()
	p.DOI = "10.1080/01621459.1963.10500830"
	p.ISBN = "9780521006019"

	e := p.ExportBibtex()

	if got := e.Fields["doi"]; got != p.DOI {
		t.Errorf("doi = %q, want %q", got, p.DOI)
	}
	if got := e.Fields["isbn"]; got != p.ISBN {
		t.Errorf("isbn = %q, want %q", got, p.ISBN)
	}
}

func TestExportBibtexPrefersTopLevelDOI(t *testing.T) {
	p := exportTestPaper()
	p.DOI = "10.1080/01621459.1963.10500830"
	p.Bibtex.Fields["doi"] = "10.9999/some-other-doi"

	e := p.ExportBibtex()

	if got := e.Fields["doi"]; got != p.DOI {
		t.Errorf("doi = %q, want top-level %q", got, p.DOI)
	}
}

func TestExportBibtexUnchangedWhenNeitherSet(t *testing.T) {
	p := exportTestPaper()

	e := p.ExportBibtex()

	if _, ok := e.Fields["doi"]; ok {
		t.Errorf("doi field present, want none: %q", e.Fields["doi"])
	}
	if _, ok := e.Fields["isbn"]; ok {
		t.Errorf("isbn field present, want none: %q", e.Fields["isbn"])
	}
	if e.Type != p.Bibtex.Type {
		t.Errorf("type = %q, want %q", e.Type, p.Bibtex.Type)
	}
	for k, v := range p.Bibtex.Fields {
		if e.Fields[k] != v {
			t.Errorf("field %s = %q, want %q", k, e.Fields[k], v)
		}
	}

	// The returned Fields map must not alias p.Bibtex.Fields: mutating it
	// must not affect the original entry.
	e.Fields["title"] = "mutated"
	if p.Bibtex.Fields["title"] == "mutated" {
		t.Error("ExportBibtex result aliases p.Bibtex.Fields")
	}
}
