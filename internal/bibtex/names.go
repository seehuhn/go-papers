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

// Package bibtex represents and parses BibTeX bibliography entries. Entry
// holds a bibtex entry's type and fields (see entry.go); ParseNames parses
// bibtex "author"/"editor" fields into structured Name values. ParseNames
// is a Go port of the `parse` function from the Python `s6py.bibtex.author`
// module, which in turn implements the name-parsing algorithm described in
// "Tame the BeaST, The B to X of BibTeX" by Nicholas Markey.
package bibtex

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Name holds the four components bibtex distinguishes in a personal name:
// First, Von, Last and Jr (e.g. "Charles Louis Xavier Joseph", "de la",
// "Vall{\'e}e Poussin", ""). Values remain bibtex-encoded - e.g.
// `Vo{\ss}` stays `Vo{\ss}` - callers that need plain text should decode
// the individual fields themselves.
type Name struct {
	First, Von, Last, Jr string
}

// Full formats n in "First Von Last, Jr" display order. The result
// remains bibtex-encoded. Empty components are omitted.
func (n Name) Full() string {
	var words []string
	if n.First != "" {
		words = append(words, n.First)
	}
	if n.Von != "" {
		words = append(words, n.Von)
	}
	if n.Last != "" {
		words = append(words, n.Last)
	}
	s := strings.Join(words, " ")
	if n.Jr == "" {
		return s
	}
	if s == "" {
		return n.Jr
	}
	return s + ", " + n.Jr
}

// ParseNames parses a bibtex "author" or "editor" field, splitting it on
// " and " into individual names and each name into First/Von/Last/Jr
// parts. Field values stay bibtex-encoded; ParseNames does not decode
// TeX macros or braces.
//
// ParseNames returns an error if an author entry is empty (e.g. two
// consecutive " and " separators with nothing between them) or if a name
// contains more than two commas.
func ParseNames(s string) ([]Name, error) {
	if s == "" {
		return nil, nil
	}

	authorTokens := splitAuthors(s)

	var res []Name
	for _, a := range authorTokens {
		parts := splitParts(a)
		if len(parts) == 0 {
			continue
		}
		name, err := buildName(parts)
		if err != nil {
			return nil, fmt.Errorf("bibtex: %w in author %q", err, strings.Join(a, ""))
		}
		res = append(res, name)
	}
	return res, nil
}

// splitAuthors splits a bibtex name-list string into per-author token
// lists. Each token is either a single rune (as a string) or an entire
// braced group (e.g. "{Barnes and Noble, Inc.}"), so that later stages
// never split, or look inside, a braced group. Authors are separated by
// " and " occurring outside any brace group.
func splitAuthors(s string) [][]string {
	var authors [][]string
	var a []string
	i := 0
	n := len(s)
	for i < n {
		switch {
		case s[i] == '{':
			i0 := i
			i++
			blevel := 1
			for blevel > 0 && i < n {
				if s[i] == '{' {
					blevel++
				} else if s[i] == '}' {
					blevel--
				}
				i++
			}
			a = append(a, s[i0:i])
		case strings.HasPrefix(s[i:], " and "):
			authors = append(authors, a)
			a = nil
			i += 5
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			a = append(a, string(r))
			i += size
		}
	}
	authors = append(authors, a)
	return authors
}

// splitParts splits one author's token list into comma-separated parts,
// each part being a list of whitespace-separated words. A "." not
// followed by "-" also forces a word break (so that abbreviations like
// "J." are recognised as separate words, while hyphenated forms like
// "J.-P." stay together). Braced groups are opaque tokens: they never
// trigger a comma/whitespace/period split themselves, but are
// concatenated into whichever word they occur in.
func splitParts(a []string) [][]string {
	var parts [][]string
	var p []string
	var w []string
	flush := func() {
		if len(w) > 0 {
			p = append(p, strings.Join(w, ""))
			w = nil
		}
	}
	for i, c := range a {
		switch {
		case c == ",":
			flush()
			parts = append(parts, p)
			p = nil
		case isSpaceToken(c) || c == "~":
			flush()
		default:
			w = append(w, c)
			if c == "." && !(i+1 < len(a) && a[i+1] == "-") {
				flush()
			}
		}
	}
	flush()
	parts = append(parts, p)
	return parts
}

// isSpaceToken reports whether c is a single whitespace rune, as
// produced by splitAuthors. Braced groups always start with "{" and are
// never treated as whitespace, even if (in some contrived case) they
// happened to contain only whitespace characters.
func isSpaceToken(c string) bool {
	if strings.HasPrefix(c, "{") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(c)
	return unicode.IsSpace(r)
}

// startsLower reports whether word's first rune is a lowercase letter.
// Braced groups start with "{", which is never lowercase, so they never
// count as the start of a "von" word.
func startsLower(word string) bool {
	r, _ := utf8.DecodeRuneInString(word)
	return unicode.IsLower(r)
}

// buildName applies the bibtex first/von/last/jr rules to the
// comma-separated parts of one author name.
func buildName(parts [][]string) (Name, error) {
	if len(parts) > 3 {
		return Name{}, fmt.Errorf("more than two commas in name")
	}

	p := parts[0]

	// von = the maximal sequence of words in p (excluding the very last
	// word, which must be part of "last") that start with a lower-case
	// letter, at brace level 0.
	var ii []int
	for i := 0; i < len(p)-1; i++ {
		if startsLower(p[i]) {
			ii = append(ii, i)
		}
	}

	var first, von, last, jr string
	switch len(parts) {
	case 1:
		// "First von Last". This is the only form where an empty p is
		// an error: von/last are derived entirely from p, so an empty p
		// leaves no word to serve as "last" (matching the Python
		// original, which raises IndexError on p[-1] here). In the
		// 2- and 3-part forms below, an empty p just yields an empty
		// "last" - not an error - because "first" (and "jr") come from
		// the other parts.
		if len(ii) > 0 {
			minI, maxI := ii[0], ii[len(ii)-1]
			first = strings.Join(p[:minI], " ")
			von = strings.Join(p[minI:maxI+1], " ")
			last = strings.Join(p[maxI+1:], " ")
		} else {
			if len(p) == 0 {
				return Name{}, fmt.Errorf("empty name")
			}
			first = strings.Join(p[:len(p)-1], " ")
			last = p[len(p)-1]
		}
	case 2:
		// "von Last, First"
		first = strings.Join(parts[1], " ")
		if len(ii) > 0 {
			maxI := ii[len(ii)-1]
			von = strings.Join(p[:maxI+1], " ")
			last = strings.Join(p[maxI+1:], " ")
		} else {
			last = strings.Join(p, " ")
		}
	default: // 3
		// "von Last, Jr, First"
		jr = strings.Join(parts[1], " ")
		first = strings.Join(parts[2], " ")
		if len(ii) > 0 {
			maxI := ii[len(ii)-1]
			von = strings.Join(p[:maxI+1], " ")
			last = strings.Join(p[maxI+1:], " ")
		} else {
			last = strings.Join(p, " ")
		}
	}
	return Name{First: first, Von: von, Last: last, Jr: jr}, nil
}
