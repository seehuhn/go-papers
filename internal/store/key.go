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

package store

import (
	"fmt"
	"os"
	"strings"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/tex"
)

// MakeKey generates a citation key from an author field and year.
// It extracts the first author's last name (without the von part),
// applies tex.Fold for normalization, keeps only [a-z-] characters,
// and formats as "name_year".
func MakeKey(authorField, year string) (string, error) {
	if authorField == "" {
		return "", fmt.Errorf("no author provided")
	}

	// Parse the author field
	names, err := bibtex.ParseNames(authorField)
	if err != nil {
		return "", fmt.Errorf("parsing author field: %w", err)
	}

	if len(names) == 0 {
		return "", fmt.Errorf("no author found in field")
	}

	// Get the first author's last name
	lastName := names[0].Last
	if lastName == "" {
		return "", fmt.Errorf("first author has no last name")
	}

	// Join multi-word last names with hyphens
	// Split by spaces and rejoin with hyphens
	words := strings.Fields(lastName)
	processedName := strings.Join(words, "-")

	// Apply tex.Fold for normalization
	folded := tex.Fold(processedName)

	// Keep only [a-z-] characters, filtering out all other characters
	var result strings.Builder
	hasLetter := false
	for _, r := range folded {
		if r >= 'a' && r <= 'z' {
			result.WriteRune(r)
			hasLetter = true
		} else if r == '-' {
			result.WriteRune(r)
		}
	}

	if !hasLetter {
		return "", fmt.Errorf("no usable letters in author name")
	}

	return result.String() + "_" + year, nil
}

// FreeKey returns an unused citation key based on the given base.
// If base is unused, it returns base. Otherwise it tries base+"a", base+"b", ..., base+"z",
// returning an error if all are taken or after 'z'.
// Existence is checked using os.Stat on the paper directory.
func (s *Store) FreeKey(base string) (string, error) {
	// Check if base itself is free
	_, err := os.Stat(s.Dir(base))
	if err != nil {
		// Directory doesn't exist, so base is free
		if os.IsNotExist(err) {
			return base, nil
		}
		// Some other error occurred
		return "", err
	}

	// Base exists, try suffixes a-z
	for suffix := 'a'; suffix <= 'z'; suffix++ {
		candidate := base + string(suffix)
		_, err := os.Stat(s.Dir(candidate))
		if err != nil {
			// Directory doesn't exist, so this candidate is free
			if os.IsNotExist(err) {
				return candidate, nil
			}
			// Some other error occurred
			return "", err
		}
	}

	// All suffixes from a-z are taken
	return "", fmt.Errorf("no free citation key available (tried %s through %sz)", base, base)
}
