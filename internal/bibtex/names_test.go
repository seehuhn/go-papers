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
	"reflect"
	"testing"
)

func TestParseNames(t *testing.T) {
	cases := []struct {
		in   string
		want []Name
	}{
		// Cases from the task-3 brief.
		{"Hoeffding, Wassily",
			[]Name{{First: "Wassily", Last: "Hoeffding"}}},
		{`Vo{\ss}, Jochen`,
			[]Name{{First: "Jochen", Last: `Vo{\ss}`}}},
		{"Jean de la Fontaine",
			[]Name{{First: "Jean", Von: "de la", Last: "Fontaine"}}},
		{"de la Fontaine, Jean",
			[]Name{{First: "Jean", Von: "de la", Last: "Fontaine"}}},
		{"Smith, Jr., John",
			[]Name{{First: "John", Last: "Smith", Jr: "Jr."}}},
		{"Smith, John and Doe, Jane",
			[]Name{{First: "John", Last: "Smith"}, {First: "Jane", Last: "Doe"}}},
		{"{Barnes and Noble, Inc.}",
			[]Name{{Last: "{Barnes and Noble, Inc.}"}}},
		{"Charles Louis Xavier Joseph de la Vall{\\'e}e Poussin",
			[]Name{{First: "Charles Louis Xavier Joseph", Von: "de la",
				Last: "Vall{\\'e}e Poussin"}}},
		{"others",
			[]Name{{Last: "others"}}},

		// Cases ported from s6py/bibtex/author_test.py (test_authors).
		// Each Python case builds a (first, von, last, jr) tuple, formats
		// it to a bibtex string with as_bibtex, then re-parses it and
		// checks the round trip. Since this package does not implement
		// as_bibtex (a later task adds the entry model), the equivalent
		// bibtex strings are given directly here, computed by hand
		// following the same as_bibtex logic.
		//
		// t0 = (("Peter",), "", "Waschendorf", "")
		// as_bibtex(*t0) == "Waschendorf, Peter"
		{"Waschendorf, Peter",
			[]Name{{First: "Peter", Last: "Waschendorf"}}},
		// t0 = (("Peter",), "v.", "Waschendorf", "")
		// as_bibtex(*t0) == "v. Waschendorf, Peter"
		{"v. Waschendorf, Peter",
			[]Name{{First: "Peter", Von: "v.", Last: "Waschendorf"}}},
		// t0 = (("Peter",), "", "Waschendorf", "Jr.")
		// as_bibtex(*t0) == "Waschendorf, Jr., Peter"
		{"Waschendorf, Jr., Peter",
			[]Name{{First: "Peter", Last: "Waschendorf", Jr: "Jr."}}},

		// Additional coverage of the original s6py.bibtex.author.parse
		// behaviour: an empty input string parses to no authors.
		{"", nil},
	}
	for _, c := range cases {
		got, err := ParseNames(c.in)
		if err != nil {
			t.Errorf("ParseNames(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseNames(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseNamesErrors(t *testing.T) {
	cases := []string{
		// An empty author entry: two consecutive " and " separators
		// (double space between them) leave nothing between them for
		// the second author.
		"John Smith and  and Jane Doe",
		// Three commas (more than two) in a single name.
		"Smith, Jr., John, Extra",
	}
	for _, in := range cases {
		got, err := ParseNames(in)
		if err == nil {
			t.Errorf("ParseNames(%q) = %+v, want error", in, got)
		}
	}
}

func TestFull(t *testing.T) {
	cases := []struct {
		n    Name
		want string
	}{
		{Name{First: "Wassily", Last: "Hoeffding"}, "Wassily Hoeffding"},
		{Name{First: "Jean", Von: "de la", Last: "Fontaine"}, "Jean de la Fontaine"},
		{Name{Last: "others"}, "others"},
		{Name{First: "John", Last: "Smith", Jr: "Jr."}, "John Smith, Jr."},
		{Name{Last: "Smith", Jr: "Jr."}, "Smith, Jr."},
	}
	for _, c := range cases {
		if got := c.n.Full(); got != c.want {
			t.Errorf("%+v.Full() = %q, want %q", c.n, got, c.want)
		}
	}
}
