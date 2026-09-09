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

// Package match provides utilities for matching and comparing strings,
// particularly for determining similarity between paper titles.
package match

import (
	"unicode"

	"seehuhn.de/go/paper/internal/tex"
)

// Tokens returns the folded alphanumeric tokens from a string.
// It folds the string using tex.Fold, then splits it into runs of
// letters and digits, dropping empty tokens.
func Tokens(s string) []string {
	folded := tex.Fold(s)
	var tokens []string
	var current []rune

	for _, r := range folded {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}

	return tokens
}

// TitleSimilarity returns the Jaccard similarity index between two strings.
// It calculates the Jaccard index of the folded alphanumeric token sets,
// returning a value in [0, 1]. Two empty sets score 0.
func TitleSimilarity(a, b string) float64 {
	tokensA := Tokens(a)
	tokensB := Tokens(b)

	// Create sets to track unique tokens and their frequencies
	setA := make(map[string]bool)
	setB := make(map[string]bool)

	for _, t := range tokensA {
		setA[t] = true
	}
	for _, t := range tokensB {
		setB[t] = true
	}

	// Count intersection
	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}

	// Count union
	union := len(setA) + len(setB) - intersection

	// Handle the case of two empty sets
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// TitleCoverage reports how much of a title a free-text reference
// carries: the fraction of the title's folded tokens that also occur in
// text, in [0, 1]. An empty title scores 0.
//
// It is asymmetric on purpose, where TitleSimilarity is not. A reference
// is typed as "Author, Title, year", so it holds words the title does
// not, and those extra words must not count against the match the way a
// Jaccard index would: "Applied Cryptography, Schneier, 1996" scores only
// 0.5 against the title "Applied Cryptography" but covers all of it.
//
// A one-token title is the exception. It occurs inside far too many
// references to mean anything on its own, so it scores 1 only when the
// text is that same single token, and 0 otherwise.
func TitleCoverage(title, text string) float64 {
	titleTokens := Tokens(title)
	if len(titleTokens) == 0 {
		return 0
	}

	inText := make(map[string]bool)
	for _, t := range Tokens(text) {
		inText[t] = true
	}

	if len(titleTokens) == 1 {
		if len(inText) == 1 && inText[titleTokens[0]] {
			return 1
		}
		return 0
	}

	// Count the distinct title tokens the text carries: a word repeated
	// in the title must not weigh more than one that is not.
	distinct := make(map[string]bool)
	for _, t := range titleTokens {
		distinct[t] = true
	}
	found := 0
	for t := range distinct {
		if inText[t] {
			found++
		}
	}
	return float64(found) / float64(len(distinct))
}
