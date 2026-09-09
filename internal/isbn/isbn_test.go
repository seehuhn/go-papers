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

package isbn

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"0-521-00601-5", "9780521006019", false},
		{"9780521006019", "9780521006019", false},
		{"ISBN 978 0 521 00601 9", "9780521006019", false},
		{"ISBN-10: 0-521-00601-5", "9780521006019", false},
		{"080442957X", "9780804429573", false},
		{"9780521006018", "", true}, // checksum
		{"12345", "", true},         // too short
		{"", "", true},              // empty
		{"978052100601９", "", true}, // fullwidth digit (U+FF19)
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("Normalize(%q) error = %v, want error = %v", c.in, err != nil, c.wantErr)
		}
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0-521-00601-5", true},
		{"9780521006019", true},
		{"ISBN 978 0 521 00601 9", true},
		{"080442957X", true},
		{"9780521006018", false}, // checksum
		{"12345", false},         // too short
		{"", false},              // empty
		{"978052100601９", false}, // fullwidth digit (U+FF19)
	}
	for _, c := range cases {
		got := Valid(c.in)
		if got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		a    string
		b    string
		want bool
	}{
		{"0-521-00601-5", "9780521006019", true},
		{"junk", "junk", true},
		{"junk", "9780521006019", false},
	}
	for _, c := range cases {
		got := Equal(c.a, c.b)
		if got != c.want {
			t.Errorf("Equal(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
