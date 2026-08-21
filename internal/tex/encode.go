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
	"golang.org/x/text/unicode/norm"
)

// accentCmd describes how to spell a combining mark as a TeX accent
// macro: the command character, and whether it is a control word (e.g.
// \H, taking a braced argument, {\H{o}}) or a control symbol (e.g. \",
// whose argument is not braced, {\"o}).
type accentCmd struct {
	ch     byte
	letter bool
}

// encodeAccent maps a combining mark to the TeX accent command that
// produces it, the inverse of letterAccentMarks and symbolAccentMarks.
var encodeAccent = func() map[rune]accentCmd {
	m := make(map[rune]accentCmd)
	for cmd, mark := range letterAccentMarks {
		m[mark] = accentCmd{ch: cmd, letter: true}
	}
	for cmd, mark := range symbolAccentMarks {
		m[mark] = accentCmd{ch: cmd, letter: false}
	}
	return m
}()

// encodeLiteral maps a single character to the TeX control word that
// expands to it, the inverse of literalMacros.
var encodeLiteral = func() map[rune]string {
	m := make(map[rune]string)
	for macro, lit := range literalMacros {
		r := []rune(lit)[0]
		if prev, ok := m[r]; ok && len(prev) <= len(macro) {
			continue // keep the shortest macro name
		}
		m[r] = macro
	}
	return m
}()

// Encode converts plain unicode text to bibtex-encoded LaTeX: accented
// letters become accent macros in braces ({\"o}, {\H{o}}), special
// letters become their macros ({\ss}, {\o}), and en/em dashes become --
// and ---. Characters with no LaTeX spelling (CJK, math symbols, ...)
// pass through unchanged; paper check's encoding rules remain the
// backstop for those.
//
// Encode is one-way: for text it fully handles, Decode(Encode(s)) == s,
// but Decode is lossy in general and is not required to be the exact
// inverse of Encode.
func Encode(s string) string {
	src := []rune(norm.NFD.String(s))
	out := make([]rune, 0, len(src))
	for _, r := range src {
		switch r {
		case '–': // en dash
			out = append(out, '-', '-')
			continue
		case '—': // em dash
			out = append(out, '-', '-', '-')
			continue
		}
		if ac, ok := encodeAccent[r]; ok && len(out) > 0 {
			base := out[len(out)-1]
			out = out[:len(out)-1]
			var macro string
			if ac.letter {
				macro = `{\` + string(ac.ch) + `{` + string(base) + `}}`
			} else {
				macro = `{\` + string(ac.ch) + string(base) + `}`
			}
			out = append(out, []rune(macro)...)
			continue
		}
		if macro, ok := encodeLiteral[r]; ok {
			out = append(out, []rune(`{\`+macro+`}`)...)
			continue
		}
		out = append(out, r)
	}
	return norm.NFC.String(string(out))
}
