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
