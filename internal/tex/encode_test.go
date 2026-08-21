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

package tex

import "testing"

func TestEncode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hoeffding, Wassily", "Hoeffding, Wassily"}, // ASCII unchanged
		{"Voß, Jochen", `Vo{\ss}, Jochen`},           // sharp s
		{"Erdős", `Erd{\H{o}}s`},                     // double acute
		{"Schrödinger", `Schr{\"o}dinger`},           // umlaut
		{"Lévy", `L{\'e}vy`},                         // acute
		{"Itô", `It{\^o}`},                           // circumflex
		{"Grüneisen–Debye", `Gr{\"u}neisen--Debye`},  // en dash -> --
		{"Łukasiewicz", `{\L}ukasiewicz`},            // stroke L
		{"Ørsted", `{\O}rsted`},                      // slashed O
		{"æther", `{\ae}ther`},                       // ligature
		{"São Paulo", `S{\~a}o Paulo`},               // tilde
		{"Čech", `{\v{C}}ech`},                       // caron
		{"Kołmogorov 数学", `Ko{\l}mogorov 数学`},        // unencodable passes through
	}
	for _, c := range cases {
		if got := Encode(c.in); got != c.want {
			t.Errorf("Encode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEncodeDecodeAgree(t *testing.T) {
	// Whatever Encode produces must Decode back to the original text.
	for _, s := range []string{"Voß", "Erdős", "Schrödinger", "Lévy", "Čech", "Łukasiewicz"} {
		enc := Encode(s)
		dec, unknown := Decode(enc)
		if len(unknown) > 0 {
			t.Errorf("Decode(Encode(%q)) reported unknown macros %v", s, unknown)
		}
		if dec != s {
			t.Errorf("Decode(Encode(%q)) = %q, want the original", s, dec)
		}
	}
}
