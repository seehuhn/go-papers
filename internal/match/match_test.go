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

package match

import (
	"testing"
)

func TestTitleSimilarity(t *testing.T) {
	cases := []struct {
		a, b     string
		min, max float64
	}{
		{"Probability inequalities for sums of bounded random variables",
			"Probability inequalities for sums of bounded random variables", 1.0, 1.0},
		{"Probability Inequalities for Sums of Bounded Random Variables",
			"probability inequalities for sums of bounded random variables.", 1.0, 1.0},
		{`A study of {SPDEs} in {G}reenland`,
			"A study of SPDEs in Greenland", 1.0, 1.0},
		{"On the KdV equation", "Semigroups of linear operators", 0.0, 0.2},
		{"Large deviations for diffusions", "Large deviations for diffusion processes", 0.5, 0.9},
	}
	for _, c := range cases {
		got := TitleSimilarity(c.a, c.b)
		if got < c.min || got > c.max {
			t.Errorf("TitleSimilarity(%q, %q) = %v, want in [%v, %v]", c.a, c.b, got, c.min, c.max)
		}
	}
}
