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
	"regexp"
	"strings"
	"unicode"
)

// BraceTitle applies the draft-time brace-protection rule to a bibtex
// title: any space-separated word carrying a capital letter beyond its
// first rune (e.g. "SPDEs", "McKean-Vlasov", "KdV", "IT") is wrapped in
// "{...}" so that bibtex/BibLaTeX styles do not lower-case it. A word
// that is already braced (starts with "{") or is already inside a braced
// group or `$...$` math run is left untouched, and math runs are never
// inspected for capitalization.
func BraceTitle(s string) string {
	words := strings.Split(s, " ")

	depth := 0
	math := false
	for i, w := range words {
		entryDepth := depth
		entryMath := math

		for _, r := range w {
			switch r {
			case '{':
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
			case '$':
				math = !math
			}
		}

		switch {
		case entryDepth > 0 || entryMath:
			// mid-group word (continuation of a braced group or math
			// run that started in an earlier word): leave untouched.
		case strings.HasPrefix(w, "{") || strings.HasPrefix(w, "$"):
			// word opens its own braced group or math run (whether it
			// closes within the same word, like "{KdV}" or "$SPDE$",
			// or spans further words): leave untouched.
		case depth > 0 || math:
			// plain word that happens to open an unterminated group:
			// leave untouched, matching the group continuation above.
		default:
			if needsBrace(w) {
				words[i] = "{" + w + "}"
			}
		}
	}

	return strings.Join(words, " ")
}

// needsBrace reports whether word carries a capital letter beyond rune
// position zero, i.e. beyond the first character of the word.
func needsBrace(word string) bool {
	pos := 0
	for _, r := range word {
		if pos >= 1 && unicode.IsUpper(r) {
			return true
		}
		pos++
	}
	return false
}

// pageRangeRE matches a single hyphen directly between two digits, e.g.
// the "-" in "13-30".
var pageRangeRE = regexp.MustCompile(`(\d)-(\d)`)

// NormalizePages normalizes a bibtex "pages" field to use an en-dash
// range ("13-30" becomes "13--30"). Strings that already contain "--"
// are left unchanged, as are non-range page values such as article
// numbers ("e1003412") or roman numerals ("xii").
func NormalizePages(s string) string {
	if strings.Contains(s, "--") {
		return s
	}
	return pageRangeRE.ReplaceAllString(s, "$1--$2")
}
