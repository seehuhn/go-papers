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
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"volume":  "58",
			"number":  "301",
			"pages":   "13--30",
			"year":    "1963",
			"doi":     "10.1080/01621459.1963.10500830",
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
