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

// Package tex implements a one-way transliteration of bibtex-style,
// LaTeX-encoded strings (e.g. `Vo{\ss}`, `{SPDEs} in {G}reenland`) into
// plain unicode text. It is a Go port of the `as_text` function from the
// Python `s6py.tex` module, extended to use combining-character accents
// instead of a literal lookup table, to cover more literal macros, to
// pass math-mode (`$...$`) content through unchanged, and to report
// unrecognized macro names instead of silently discarding them.
package tex

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// accentMarks maps a single-byte TeX accent command character to the
// unicode combining mark it represents. The base letter is combined with
// this mark and then NFC-normalized, so that precomposed characters are
// used whenever one exists.
var accentMarks = map[byte]rune{
	'`':  0x0300, // combining grave accent
	'\'': 0x0301, // combining acute accent
	'^':  0x0302, // combining circumflex accent
	'"':  0x0308, // combining diaeresis
	'~':  0x0303, // combining tilde
	'H':  0x030B, // combining double acute accent (Hungarian)
	'c':  0x0327, // combining cedilla
	'v':  0x030C, // combining caron
	'u':  0x0306, // combining breve
	'=':  0x0304, // combining macron
	'.':  0x0307, // combining dot above
}

// literalMacros maps TeX control words (backslash followed by one or more
// letters) that expand to a single literal character.
var literalMacros = map[string]string{
	"ss": "ß",
	"o":  "ø",
	"O":  "Ø",
	"ae": "æ",
	"AE": "Æ",
	"aa": "å",
	"AA": "Å",
	"l":  "ł",
	"L":  "Ł",
}

// controlSymbols maps TeX control symbols (backslash followed by exactly
// one non-letter character) that expand to a single literal character.
var controlSymbols = map[byte]string{
	'&': "&",
	'%': "%",
	'_': "_",
	'#': "#",
	'{': "{",
	'}': "}",
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// noBreakSpace is U+00A0 NO-BREAK SPACE, the expansion of TeX's `~`.
const noBreakSpace = ' '

// Decode converts a bibtex-encoded, LaTeX-flavoured string to unicode
// text. Braces used purely for grouping are stripped. Content between a
// matched pair of `$` delimiters is treated as math mode: it is copied
// verbatim, including the dollar signs, and is never inspected for
// macros; an unmatched `$` is copied as-is. Any macro name that Decode
// does not recognize is reported in unknown (empty/nil when the input
// contains no unrecognized macros); the macro's braced argument, if any,
// is still emitted (after further decoding).
func Decode(s string) (text string, unknown []string) {
	var out strings.Builder
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '$':
			if j := strings.IndexByte(s[i+1:], '$'); j >= 0 {
				end := i + 1 + j + 1
				out.WriteString(s[i:end])
				i = end
			} else {
				out.WriteByte('$')
				i++
			}
		case c == '~':
			out.WriteRune(noBreakSpace)
			i++
		case c == '{' || c == '}':
			i++
		case c == '\\':
			newI, repl, unk := decodeMacro(s, i)
			out.WriteString(repl)
			if unk != "" {
				unknown = append(unknown, unk)
			}
			i = newI
		case c == '-':
			switch {
			case strings.HasPrefix(s[i:], "---"):
				out.WriteRune('—') // em dash
				i += 3
			case strings.HasPrefix(s[i:], "--"):
				out.WriteRune('–') // en dash
				i += 2
			default:
				out.WriteByte('-')
				i++
			}
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			out.WriteRune(r)
			i += size
		}
	}
	return out.String(), unknown
}

// decodeMacro decodes the TeX macro starting at s[i] (s[i] == '\\') and
// returns the index just past the macro, the text it expands to, and,
// if the macro name was not recognized, the macro name itself.
func decodeMacro(s string, i int) (newI int, repl string, unknown string) {
	n := len(s)
	if i+1 >= n {
		// A trailing, dangling backslash: drop it.
		return i + 1, "", ""
	}

	// Accent macros: \<accent>{<letter>} or \<accent><letter>.
	if mark, ok := accentMarks[s[i+1]]; ok {
		rest := s[i+2:]
		if len(rest) >= 3 && rest[0] == '{' && isASCIILetter(rest[1]) && rest[2] == '}' {
			composed := norm.NFC.String(string(rest[1]) + string(mark))
			return i + 5, composed, ""
		}
		if len(rest) >= 1 && isASCIILetter(rest[0]) {
			composed := norm.NFC.String(string(rest[0]) + string(mark))
			return i + 3, composed, ""
		}
		// Not a well-formed accent construct; fall through and treat
		// s[i+1] as an ordinary macro name character.
	}

	// Control words: backslash followed by one or more letters. The
	// macro name ends at the first non-letter (word-boundary rule).
	if isASCIILetter(s[i+1]) {
		j := i + 1
		for j < n && isASCIILetter(s[j]) {
			j++
		}
		name := s[i+1 : j]
		if lit, ok := literalMacros[name]; ok {
			return j, lit, ""
		}
		return j, "", name
	}

	// Control symbols: backslash followed by exactly one non-letter
	// character.
	ch := s[i+1]
	if lit, ok := controlSymbols[ch]; ok {
		return i + 2, lit, ""
	}
	return i + 2, "", string(ch)
}

// mnRemover strips unicode combining marks (category Mn) from a string.
var mnRemover = runes.Remove(runes.In(unicode.Mn))

// foldReplacer replaces letters that survive combining-mark removal but
// still are not plain ASCII, before lowercasing.
var foldReplacer = strings.NewReplacer(
	"ß", "ss",
	"æ", "ae",
	"ø", "o",
	"ł", "l",
)

// Fold decodes s (see Decode) and then reduces it to a normalized,
// case-, diacritic- and accent-insensitive form suitable for search
// matching: unicode NFD normalization, combining-mark removal, the
// ß/æ/ø/ł substitutions above, and lowercasing.
func Fold(s string) string {
	decoded, _ := Decode(s)
	nfd := norm.NFD.String(decoded)
	stripped, _, err := transform.String(mnRemover, nfd)
	if err != nil {
		stripped = nfd
	}
	replaced := foldReplacer.Replace(stripped)
	return strings.ToLower(replaced)
}
