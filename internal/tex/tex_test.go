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

import (
	"reflect"
	"testing"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		in, want string
		unknown  []string
	}{
		{`hello world`, `hello world`, nil},
		{`{SPDEs} in {G}reenland`, `SPDEs in Greenland`, nil},
		{`Vo{\ss}`, `Voß`, nil},
		{`Vo{\"s}`, "Vos̈", nil}, // no precomposed s-diaeresis; combining mark survives NFC
		{`\"o and \'{e}`, `ö and é`, nil},
		{`\H{o}`, `ő`, nil},
		{`Fran\c{c}ois`, `François`, nil},
		{`A---B--C`, `A—B–C`, nil},
		{`a~b`, "a b", nil},
		{`\&`, `&`, nil},
		{`$L^2$-convergence`, `$L^2$-convergence`, nil}, // math verbatim
		{`$\alpha$-stable`, `$\alpha$-stable`, nil},     // no unknown macro in math
		{`\foo{bar}`, `bar`, []string{"foo"}},
		{`Stra\ss e`, `Straße`, nil},          // control word absorbs trailing whitespace
		{`Gau{\ss}`, `Gauß`, nil},             // braced form unaffected by whitespace rule
		{`\Hello`, ``, []string{"Hello"}},     // greedy tokenization: not \H + "ello"
		{`\Hello{x}`, `x`, []string{"Hello"}}, // ditto, with a braced argument
		{`\H o`, `ő`, nil},                    // bare accent letter, space-separated
		{`\v{c}`, `č`, nil},                   // caron accent
		{`\u{g}`, `ğ`, nil},                   // breve accent
		{`\%`, `%`, nil},                      // control symbol
		{`\_`, `_`, nil},                      // control symbol
		{`\#`, `#`, nil},                      // control symbol
		{`\{`, `{`, nil},                      // control symbol
		{`\}`, `}`, nil},                      // control symbol
		{`\@`, ``, []string{"@"}},             // unrecognized control symbol: reported unknown, char consumed
	}
	for _, c := range cases {
		got, unknown := Decode(c.in)
		if got != c.want {
			t.Errorf("Decode(%q) = %q, want %q", c.in, got, c.want)
		}
		if !reflect.DeepEqual(unknown, c.unknown) && !(len(unknown) == 0 && len(c.unknown) == 0) {
			t.Errorf("Decode(%q) unknown = %v, want %v", c.in, unknown, c.unknown)
		}
	}
}

func TestFold(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Vo{\ss}`, `voss`},
		{`L\'evy`, `levy`},
		{`{McKean}--{V}lasov`, `mckean–vlasov`},
		{`\AE`, `ae`},        // uppercase literal macro folds correctly
		{`\O`, `o`},          // uppercase literal macro folds correctly
		{`\L`, `l`},          // uppercase literal macro folds correctly
		{`Ørsted`, `orsted`}, // already-unicode uppercase Ø, not a macro
	}
	for _, c := range cases {
		if got := Fold(c.in); got != c.want {
			t.Errorf("Fold(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFoldTieMatchesSpace(t *testing.T) {
	if got := Fold(`van~Kampen`); got != "van kampen" {
		t.Errorf("Fold(van~Kampen) = %q, want %q", got, "van kampen")
	}
}
