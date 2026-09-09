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

func TestTitleCoverage(t *testing.T) {
	cases := []struct {
		title, text string
		min, max    float64
	}{
		// The reference shape resolve documents: the title plus an
		// author and a year, which must not count against the match.
		{"Applied Cryptography", "Applied Cryptography, Schneier, 1996", 1.0, 1.0},
		{"Applied Cryptography", "Bruce Schneier, Applied Cryptography, 2nd ed., 1996", 1.0, 1.0},
		{`A study of {SPDEs} in {G}reenland`, "Voss, A study of SPDEs in Greenland, 2024", 1.0, 1.0},
		{"A Book About Widgets", "A Book About Widgets", 1.0, 1.0},
		// Two of five title tokens are missing from the text.
		{"Large deviations for diffusion processes", "Large deviations for diffusions", 0.55, 0.65},
		{"Probability and Measure", "Applied Cryptography, Schneier, 1996", 0.0, 0.0},
		// A one-token title occurs in far too much to be trusted, so it
		// counts only when the text is that one token and nothing else.
		{"Ulysses", "Ulysses, Joyce, 1922", 0.0, 0.0},
		{"Ulysses", "ulysses.", 1.0, 1.0},
		{"", "anything at all", 0.0, 0.0},
	}
	for _, c := range cases {
		got := TitleCoverage(c.title, c.text)
		if got < c.min || got > c.max {
			t.Errorf("TitleCoverage(%q, %q) = %v, want in [%v, %v]", c.title, c.text, got, c.min, c.max)
		}
	}
}
