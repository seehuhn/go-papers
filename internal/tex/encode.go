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
	"unicode"

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
		if prev, ok := m[r]; ok {
			// Deterministic tie-break, independent of map iteration
			// order: prefer the shorter macro name, and on a length
			// tie prefer the lexicographically smaller one.
			if len(macro) > len(prev) {
				continue
			}
			if len(macro) == len(prev) && macro >= prev {
				continue
			}
		}
		m[r] = macro
	}
	return m
}()

// texSpecials are the characters that LaTeX reads as syntax rather than
// text and that Encode therefore escapes with a backslash. Decode's
// controlSymbols table maps each of them back, so the round trip holds.
// The remaining specials are left alone: braces and $ are the encoding's
// own grouping and math delimiters, ~ and ^ arise from accent macros, and
// a backslash in the input is already meaningful to the caller.
var texSpecials = map[rune]bool{
	'&': true,
	'%': true,
	'#': true,
	'_': true,
}

// Encode converts plain unicode text to bibtex-encoded LaTeX: accented
// letters become accent macros in braces ({\"o}, {\H{o}}), special
// letters become their macros ({\ss}, {\o}), en/em dashes become -- and
// ---, and the TeX special characters & % # _ are escaped. Characters
// with no LaTeX spelling (CJK, math symbols, ...) pass through unchanged;
// paper check's encoding rules remain the backstop for those.
//
// Encode is one-way: for text it fully handles, Decode(Encode(s)) == s,
// but Decode is lossy in general and is not required to be the exact
// inverse of Encode.
func Encode(s string) string {
	src := []rune(norm.NFD.String(s))
	out := make([]rune, 0, len(src))
	i := 0
	n := len(src)
	for i < n {
		switch src[i] {
		case '–': // en dash
			out = append(out, '-', '-')
			i++
			continue
		case '—': // em dash
			out = append(out, '-', '-', '-')
			i++
			continue
		}
		if texSpecials[src[i]] {
			out = append(out, '\\', src[i])
			i++
			continue
		}

		// Gather the NFD cluster: a base rune plus every combining
		// mark (category Mn) immediately following it. Clusters are
		// encoded as a whole so that a base with two or more stacked
		// marks - which no single accent macro can represent - is
		// recognized and passed through rather than corrupted into
		// malformed LaTeX.
		j := i + 1
		for j < n && unicode.Is(unicode.Mn, src[j]) {
			j++
		}
		out = append(out, encodeCluster(src[i:j])...)
		i = j
	}
	return norm.NFC.String(string(out))
}

// encodeCluster encodes one NFD cluster (a base rune plus zero or more
// trailing combining marks) as LaTeX. It never emits malformed LaTeX: a
// cluster it cannot fully represent as a single accent or literal macro
// is passed through unchanged (NFC-recomposed).
func encodeCluster(cluster []rune) []rune {
	// First, see whether the whole cluster composes into a single
	// character with its own literal macro (e.g. a+U+030A -> å ->
	// {\aa}). This must be checked before the single-mark accent case
	// below, since some literal targets (å, Å) arise from NFD
	// decomposition just like accented letters do.
	composed := []rune(norm.NFC.String(string(cluster)))
	if len(composed) == 1 {
		if macro, ok := encodeLiteral[composed[0]]; ok {
			return []rune(`{\` + macro + `}`)
		}
	}

	// A base rune with exactly one combining mark that is a known TeX
	// accent becomes an accent macro.
	if len(cluster) == 2 {
		if ac, ok := encodeAccent[cluster[1]]; ok {
			base := cluster[0]
			if ac.letter {
				return []rune(`{\` + string(ac.ch) + `{` + string(base) + `}}`)
			}
			return []rune(`{\` + string(ac.ch) + string(base) + `}`)
		}
	}

	// Anything else - a bare base rune, multiple stacked marks, an
	// unrecognized mark, or a mark with no base - passes through
	// unencoded, recomposed so it stays a normal precomposed
	// character rather than dangling combining marks.
	return composed
}
