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
	"strings"
	"testing"
)

func TestIsArxivDOI(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"10.48550/arXiv.2412.05039", true},
		{"10.48550/arXiv.2412.05039v2", true},
		{"10.1080/01621459.1963.10500830", false},
		{"", false},
		{"10.48550/notarxiv.1", false},
	}
	for _, c := range cases {
		if got := IsArxivDOI(c.in); got != c.want {
			t.Errorf("IsArxivDOI(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRecordIdentifiersEmptyArgsChangeNothing(t *testing.T) {
	p := &Paper{DOI: "10.1/a", ISBN: "9780521006019"}
	details, err := p.RecordIdentifiers("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(details) != 0 {
		t.Errorf("details = %v, want none", details)
	}
	if p.DOI != "10.1/a" || p.ISBN != "9780521006019" {
		t.Errorf("p mutated: DOI=%q ISBN=%q", p.DOI, p.ISBN)
	}
}

func TestRecordIdentifiersRecordsMissingDOI(t *testing.T) {
	p := &Paper{}
	details, err := p.RecordIdentifiers("10.1/a", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DOI != "10.1/a" {
		t.Errorf("DOI = %q, want 10.1/a", p.DOI)
	}
	if len(details) != 1 || !strings.Contains(details[0], "10.1/a") {
		t.Errorf("details = %v, want one entry naming 10.1/a", details)
	}
}

func TestRecordIdentifiersSameDOICaseInsensitiveChangesNothing(t *testing.T) {
	p := &Paper{DOI: "10.1/A"}
	details, err := p.RecordIdentifiers("10.1/a", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(details) != 0 {
		t.Errorf("details = %v, want none", details)
	}
	if p.DOI != "10.1/A" {
		t.Errorf("DOI = %q, want unchanged 10.1/A", p.DOI)
	}
}

func TestRecordIdentifiersReplacesArxivDOI(t *testing.T) {
	p := &Paper{DOI: "10.48550/arXiv.2412.05039"}
	details, err := p.RecordIdentifiers("10.1080/01621459.1963.10500830", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("DOI = %q, want the non-arXiv DOI", p.DOI)
	}
	if len(details) != 1 ||
		!strings.Contains(details[0], "10.48550/arXiv.2412.05039") ||
		!strings.Contains(details[0], "10.1080/01621459.1963.10500830") {
		t.Errorf("details = %v, want one entry naming both DOIs", details)
	}
}

func TestRecordIdentifiersDOIConflictIsError(t *testing.T) {
	p := &Paper{DOI: "10.1/a"}
	details, err := p.RecordIdentifiers("10.1/b", "")
	if err == nil {
		t.Fatal("want error for conflicting DOI")
	}
	if !strings.Contains(err.Error(), "10.1/a") || !strings.Contains(err.Error(), "10.1/b") {
		t.Errorf("error = %v, want it to name both DOIs", err)
	}
	if details != nil {
		t.Errorf("details = %v, want none on error", details)
	}
	if p.DOI != "10.1/a" {
		t.Errorf("DOI = %q, want unchanged on error", p.DOI)
	}
}

func TestRecordIdentifiersRecordsMissingISBNNormalised(t *testing.T) {
	p := &Paper{}
	details, err := p.RecordIdentifiers("", "0-521-00601-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want normalised 9780521006019", p.ISBN)
	}
	if len(details) != 1 || !strings.Contains(details[0], "9780521006019") {
		t.Errorf("details = %v, want one entry naming the normalised ISBN", details)
	}
}

func TestRecordIdentifiersSameISBNDifferentFormChangesNothing(t *testing.T) {
	p := &Paper{ISBN: "9780521006019"}
	details, err := p.RecordIdentifiers("", "0-521-00601-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(details) != 0 {
		t.Errorf("details = %v, want none", details)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want unchanged", p.ISBN)
	}
}

func TestRecordIdentifiersISBNConflictIsError(t *testing.T) {
	p := &Paper{ISBN: "9780521006019"}
	details, err := p.RecordIdentifiers("", "080442957X")
	if err == nil {
		t.Fatal("want error for conflicting ISBN")
	}
	if !strings.Contains(err.Error(), "9780521006019") || !strings.Contains(err.Error(), "080442957X") {
		t.Errorf("error = %v, want it to name both ISBNs", err)
	}
	if details != nil {
		t.Errorf("details = %v, want none on error", details)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want unchanged on error", p.ISBN)
	}
}

func TestRecordIdentifiersInvalidISBNIsError(t *testing.T) {
	p := &Paper{}
	details, err := p.RecordIdentifiers("", "not-an-isbn")
	if err == nil {
		t.Fatal("want error for invalid ISBN")
	}
	if details != nil {
		t.Errorf("details = %v, want none", details)
	}
	if p.ISBN != "" {
		t.Errorf("ISBN = %q, want unchanged on error", p.ISBN)
	}
}

func TestRecordIdentifiersInvalidISBNDoesNotRecordDOI(t *testing.T) {
	// A DOI-and-ISBN call where the ISBN fails validation must write
	// neither identifier: nothing is written on error.
	p := &Paper{}
	_, err := p.RecordIdentifiers("10.1/a", "not-an-isbn")
	if err == nil {
		t.Fatal("want error for invalid ISBN")
	}
	if p.DOI != "" {
		t.Errorf("DOI = %q, want unchanged because the call errored", p.DOI)
	}
}

func TestRecordIdentifiersBothMissing(t *testing.T) {
	p := &Paper{}
	details, err := p.RecordIdentifiers("10.1/a", "0-521-00601-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DOI != "10.1/a" {
		t.Errorf("DOI = %q, want 10.1/a", p.DOI)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want normalised 9780521006019", p.ISBN)
	}
	if len(details) != 2 {
		t.Errorf("details = %v, want two entries", details)
	}
}
