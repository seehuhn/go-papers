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

package bibtex

import (
	"os"
	"testing"
)

func TestFormatGolden(t *testing.T) {
	e := Entry{
		Type: "article",
		Fields: map[string]string{
			// Fields deliberately not in FieldOrder sequence to test deterministic sorting
			"doi":     "10.1080/01621459.1963.10500830",
			"author":  "Hoeffding, Wassily",
			"year":    "1963",
			"title":   "Probability inequalities for sums of bounded random variables",
			"pages":   "13--30",
			"journal": "Journal of the American Statistical Association",
			"number":  "301",
			"volume":  "58",
		},
	}
	got := Format("hoeffding_1963", e)
	want, err := os.ReadFile("testdata/hoeffding_1963.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("Format mismatch:\ngot:\n%s\nwant:\n%s", got, string(want))
	}
}

func TestRequiredFields(t *testing.T) {
	if _, ok := RequiredFields["article"]; !ok {
		t.Fatal("no required fields for article")
	}
}

// TestRequiredFieldsIncludesStandardTypes pins Finding 3: inbook,
// proceedings, manual, booklet and conference are standard BibTeX entry
// types and must not be rejected as unknown.
func TestRequiredFieldsIncludesStandardTypes(t *testing.T) {
	for _, typ := range []string{"inbook", "proceedings", "manual", "booklet", "conference"} {
		if _, ok := RequiredFields[typ]; !ok {
			t.Errorf("no required fields for standard type %q", typ)
		}
	}
}

func TestRequiredFieldsConferenceMatchesInproceedings(t *testing.T) {
	if got, want := RequiredFields["conference"], RequiredFields["inproceedings"]; len(got) != len(want) {
		t.Errorf("conference required fields = %v, want same as inproceedings %v", got, want)
	}
}

func TestFormatUnknownFields(t *testing.T) {
	// Test that unknown fields are sorted alphabetically after known fields.
	// This test uses unknown fields (zzz, abstract, mrnumber) mixed with known ones.
	e := Entry{
		Type: "article",
		Fields: map[string]string{
			// Known fields
			"author": "Smith, John",
			"title":  "A Test Article",
			"year":   "2024",
			// Unknown fields in intentionally mixed order in the map
			"zzz":      "last alphabetically",
			"abstract": "first alphabetically",
			"mrnumber": "middle alphabetically",
			"journal":  "Test Journal",
		},
	}
	got := Format("test_key", e)

	// Expected output: known fields in FieldOrder, then unknown fields alphabetically
	want := `@article{test_key,
  author = {Smith, John},
  title = {A Test Article},
  journal = {Test Journal},
  year = {2024},
  abstract = {first alphabetically},
  mrnumber = {middle alphabetically},
  zzz = {last alphabetically},
}
`

	if got != want {
		t.Errorf("Format mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
