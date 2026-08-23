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

// Package doi is the single home for DOI syntax and extraction, used by
// internal/store, internal/sources and internal/pdfid, which used to
// each carry their own copy of the same regular expression.
//
// That shared regex excluded punctuation - '"<>' - from the DOI suffix,
// which truncates legacy SICI DOIs such as
// "10.1002/(SICI)1097-0258(19960229)15:4<361::AID-SIM168>3.0.CO;2-4" at
// the first '<'. Per the DOI Handbook the suffix is opaque: no character
// class can exclude punctuation without also excluding some legal DOI.
// This package instead offers two different operations for the two
// different situations a DOI-shaped string is found in:
//
//   - Syntactic validates a STATED value (a bibtex doi field, a CLI
//     argument, a resolved doi.org URL) as a whole - no trimming, since
//     the caller already knows the whole value is meant to be the DOI.
//   - Candidates scans PROSE for DOI-shaped runs and, for each, offers a
//     ladder of progressively less-trimmed-of-trailing-punctuation
//     candidates, longest first, for a caller to validate against an
//     authority (see internal/pdfid's tier 2) and accept the first
//     confirmed hit.
package doi

import (
	"regexp"
	"strings"
)

// registrant matches a DOI registrant code: "10." followed by a 4-9
// digit prefix, optionally sub-divided by further dot-separated digit
// groups (e.g. "10.1000.10", used by some publishers to identify an
// imprint or series within their main prefix).
const registrant = `10\.\d{4,9}(?:\.\d+)*`

// syntacticPattern anchors registrant, a "/", and a non-empty suffix of
// non-whitespace bytes across the whole string. No suffix character is
// excluded: the DOI Handbook makes the suffix opaque, so any character
// class would truncate some legal DOI (as the old doiPattern truncated
// SICI DOIs at their first '<').
var syntacticPattern = regexp.MustCompile(`^` + registrant + `/\S+$`)

// Syntactic reports whether s is a whole, syntactically well-formed DOI:
// "10." plus a registrant code (optionally sub-divided, e.g.
// "10.1000.10"), a "/", and a non-empty suffix of non-whitespace bytes.
//
// Use Syntactic on a STATED DOI, where the whole value is the candidate
// and nothing is trimmed: a bibtex doi field, a CLI -doi argument, or a
// doi.org URL's path with the DOI prefix already removed. For a DOI
// embedded in prose, where the surrounding text might have glued on
// trailing punctuation, use Candidates instead.
func Syntactic(s string) bool {
	return syntacticPattern.MatchString(s)
}

// candidatePattern finds a greedy DOI-shaped run in prose: a registrant,
// a "/", and everything up to the next whitespace (or the end of the
// text). It never excludes a suffix character, so it never truncates a
// SICI DOI the way the old doiRegex did; that is the caller's job via
// the trim ladder in Candidates, not this pattern's.
var candidatePattern = regexp.MustCompile(registrant + `/\S+`)

// trimChars are the trailing punctuation bytes Candidates strips one at
// a time when building a match's trim ladder. This is deliberately the
// same small set of sentence/list punctuation regardless of context; a
// trailing ')' is handled separately, since whether it belongs to the
// DOI (a balanced SICI-style suffix) or the sentence (an unbalanced
// parenthetical) cannot be decided by a fixed character class.
const trimChars = `.,;:!?'"`

// Candidates scans text for DOI-shaped runs and returns, for each in the
// order it was found, a trim ladder longest first: the raw greedy match,
// then that match with exactly one trailing punctuation byte removed,
// repeated until the trailing byte is no longer punctuation (or, for a
// trailing ')', is balanced within the candidate - see the package
// doc). All of a match's ladder rungs appear together, in ladder order,
// before the next match's rungs begin.
//
// Every returned candidate satisfies Syntactic: trimming only ever
// removes trailing punctuation bytes, never bytes that were required for
// candidatePattern to match in the first place, so the suffix never
// empties out.
//
// Longest first matters to a caller validating this ladder and stopping
// at the first confirmed hit: both "10.1234/abc.v2" and "10.1234/abc"
// can be separately registered DOIs, and validating shortest first would
// return the wrong one whenever the longer form is also real.
func Candidates(text string) []string {
	var out []string
	for _, m := range candidatePattern.FindAllString(text, -1) {
		out = append(out, ladder(m)...)
	}
	return out
}

// ladder returns match's trim ladder, longest (match itself) first, by
// repeatedly stripping one trailing punctuation byte at a time; see
// Candidates.
func ladder(match string) []string {
	out := []string{match}
	cur := match
	for {
		next, ok := trimOne(cur)
		if !ok {
			return out
		}
		out = append(out, next)
		cur = next
	}
}

// trimOne strips exactly one trailing punctuation byte from s, if s ends
// in one, and reports whether it did. A trailing ')' is stripped only
// when it is unbalanced within s - i.e. s has no earlier '(' to match it
// - since prose wraps a DOI in parentheses far more often than a DOI's
// own opaque suffix ends in one unmatched ')'; a balanced trailing ')',
// as in a SICI DOI's "...3.0.CO;2-4)"-shaped suffix, is left alone. Any
// other byte in trimChars is always stripped.
func trimOne(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	last := s[len(s)-1]
	switch {
	case last == ')':
		if balanced(s) {
			return "", false
		}
	case strings.IndexByte(trimChars, last) < 0:
		return "", false
	}
	return s[:len(s)-1], true
}

// balanced reports whether s has an equal count of '(' and ')' - used to
// decide whether s's trailing ')' closes an earlier '(' within s (part
// of the DOI's own suffix) or is unmatched (prose wrapping the DOI in a
// parenthetical).
func balanced(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth == 0
}
