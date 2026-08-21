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

import "testing"

func TestBraceTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Probability inequalities for sums", "Probability inequalities for sums"},
		{"A study of SPDEs in Greenland", "A study of {SPDEs} in Greenland"},
		{"On McKean-Vlasov equations", "On {McKean-Vlasov} equations"},
		{"The KdV equation", "The {KdV} equation"},
		{"On the {KdV} equation", "On the {KdV} equation"}, // already braced: unchanged
		{"IT and society", "{IT} and society"},
		{"First word untouched", "First word untouched"},             // leading cap at pos 1 only
		{`Solutions of $SPDE$ models`, `Solutions of $SPDE$ models`}, // math mode untouched
	}
	for _, c := range cases {
		if got := BraceTitle(c.in); got != c.want {
			t.Errorf("BraceTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePages(t *testing.T) {
	cases := []struct{ in, want string }{
		{"13-30", "13--30"},
		{"13--30", "13--30"},
		{"e1003412", "e1003412"},
		{"xii", "xii"},
	}
	for _, c := range cases {
		if got := NormalizePages(c.in); got != c.want {
			t.Errorf("NormalizePages(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
