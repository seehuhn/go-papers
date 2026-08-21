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

package resolve

import (
	"testing"

	"seehuhn.de/go/paper/internal/pdfid"
)

// fullPrism is the publisher metadata of the Hoeffding fixture, as a
// PRISM packet in the PDF would carry it, with values deliberately
// different from the Crossref ones so that a wrongly overwritten field
// is visible.
func fullPrism() *pdfid.PrismInfo {
	return &pdfid.PrismInfo{
		DOI:             "10.1080/01621459.1963.10500830",
		PublicationName: "J. Amer. Statist. Assoc.",
		ISSN:            "0162-1459",
		Volume:          "999",
		Number:          "999",
		Pages:           "999-1000",
		CoverDate:       "1963-03-01",
		AggregationType: "journal",
	}
}

// TestFillFromPrismFillsGaps checks that fields Crossref did not supply
// are taken from the PRISM metadata.
func TestFillFromPrismFillsGaps(t *testing.T) {
	w := hoeffdingWork()
	w.ContainerTitle = nil
	w.Volume = ""
	w.Issue = ""
	w.Page = ""

	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	FillFromPrism(p, fullPrism())

	f := p.Bibtex.Fields
	want := map[string]string{
		"journal": "J. Amer. Statist. Assoc.",
		"volume":  "999",
		"number":  "999",
		"pages":   "999--1000", // normalized to the bibtex en-dash range
		"issn":    "0162-1459",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %s = %q, want %q", k, f[k], v)
		}
	}
}

// TestFillFromPrismKeepsCrossref checks that Crossref stays
// authoritative: a field the Crossref record supplied is never
// overwritten from PRISM.
func TestFillFromPrismKeepsCrossref(t *testing.T) {
	p, err := FromCrossref(hoeffdingWork())
	if err != nil {
		t.Fatal(err)
	}
	FillFromPrism(p, fullPrism())

	f := p.Bibtex.Fields
	want := map[string]string{
		"journal": "Journal of the American Statistical Association",
		"volume":  "58",
		"number":  "301",
		"pages":   "13--30",
		"issn":    "0162-1459", // Crossref supplied no ISSN, so PRISM fills it
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %s = %q, want %q", k, f[k], v)
		}
	}
}

// TestFillFromPrismEncodesTeX checks that the journal name goes through
// tex.Encode, like every other draft field.
func TestFillFromPrismEncodesTeX(t *testing.T) {
	w := hoeffdingWork()
	w.ContainerTitle = nil

	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	FillFromPrism(p, &pdfid.PrismInfo{PublicationName: "Annalen für Mathematik"})

	if got := p.Bibtex.Fields["journal"]; got != `Annalen f{\"u}r Mathematik` {
		t.Errorf("journal = %q, want the tex-encoded name", got)
	}
}

// TestFillFromPrismNil checks that a document without PRISM metadata
// leaves the entry exactly as Crossref built it.
func TestFillFromPrismNil(t *testing.T) {
	p, err := FromCrossref(hoeffdingWork())
	if err != nil {
		t.Fatal(err)
	}
	before := len(p.Bibtex.Fields)

	FillFromPrism(p, nil)
	FillFromPrism(nil, fullPrism()) // must not panic

	if len(p.Bibtex.Fields) != before {
		t.Errorf("field count = %d, want %d unchanged", len(p.Bibtex.Fields), before)
	}
}
