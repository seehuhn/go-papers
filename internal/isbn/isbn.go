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

// Package isbn is the single home for ISBN syntax and normalization.
package isbn

import (
	"errors"
	"fmt"
	"strings"
)

// Normalize returns the canonical ISBN-13 (13 digits, no hyphens) for s,
// accepting ISBN-10 or ISBN-13 with or without hyphens/spaces and a
// leading "ISBN", "ISBN-10:" or "ISBN-13:" label. It returns an error
// when the digits do not form an ISBN or the checksum fails.
func Normalize(s string) (string, error) {
	// Strip a leading "ISBN", "ISBN-10:" or "ISBN-13:" label. The
	// qualified forms come first, so that "ISBN" never matches half of
	// one and leaves the rest of the label behind.
	s = strings.TrimSpace(s)
	for _, label := range []string{"ISBN-10:", "ISBN-13:", "ISBN"} {
		if rest, ok := strings.CutPrefix(s, label); ok {
			s = strings.TrimSpace(rest)
			break
		}
	}

	// Remove hyphens and spaces, extract digits
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		} else if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			// Separators, skip
		} else if (r == 'X' || r == 'x') && len(digits) == 9 {
			// X is only valid as the 10th digit in ISBN-10
			digits = append(digits, 'X')
		} else {
			return "", fmt.Errorf("isbn: invalid character %q", r)
		}
	}

	// Check length
	if len(digits) != 10 && len(digits) != 13 {
		return "", errors.New("isbn: invalid length")
	}

	// Validate and convert
	if len(digits) == 10 {
		// Validate ISBN-10 checksum
		if !validateISBN10(digits) {
			return "", errors.New("isbn: invalid checksum")
		}
		// Convert to ISBN-13
		return convertISBN10to13(digits), nil
	}

	// Validate ISBN-13 checksum
	if !validateISBN13(digits) {
		return "", errors.New("isbn: invalid checksum")
	}
	// Return as string
	result := make([]byte, 13)
	for i, r := range digits {
		result[i] = byte(r)
	}
	return string(result), nil
}

// validateISBN10 checks if the ISBN-10 checksum is valid.
// The check digit uses weights 10..1 modulo 11, with X = 10.
func validateISBN10(digits []rune) bool {
	if len(digits) != 10 {
		return false
	}

	sum := 0
	for i := 0; i < 9; i++ {
		digit := int(digits[i] - '0')
		sum += digit * (10 - i)
	}

	checkDigit := int((11 - (sum % 11)) % 11)
	expected := 0
	if digits[9] == 'X' {
		expected = 10
	} else {
		expected = int(digits[9] - '0')
	}

	return checkDigit == expected
}

// isbn13CheckDigit computes the check digit for the first 12 digits of an ISBN-13.
// The check digit uses alternating weights 1,3.
func isbn13CheckDigit(digits []byte) int {
	sum := 0
	for i := 0; i < len(digits); i++ {
		digit := int(digits[i] - '0')
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += digit * weight
	}
	return (10 - (sum % 10)) % 10
}

// validateISBN13 checks if the ISBN-13 checksum is valid.
// The check digit uses alternating weights 1,3.
func validateISBN13(digits []rune) bool {
	if len(digits) != 13 {
		return false
	}

	// Convert first 12 digits to bytes for the helper
	bytes12 := make([]byte, 12)
	for i := 0; i < 12; i++ {
		bytes12[i] = byte(digits[i])
	}

	checkDigit := isbn13CheckDigit(bytes12)
	expected := int(digits[12] - '0')

	return checkDigit == expected
}

// convertISBN10to13 converts an ISBN-10 to ISBN-13.
// It prefixes "978" and recomputes the check digit.
func convertISBN10to13(digits []rune) string {
	// Extract the first 9 digits of ISBN-10 (without check digit)
	prefix := "978"
	for i := 0; i < 9; i++ {
		prefix += string(digits[i])
	}

	// Compute the check digit for ISBN-13
	bytes12 := make([]byte, 12)
	for i := 0; i < 12; i++ {
		bytes12[i] = prefix[i]
	}
	checkDigit := isbn13CheckDigit(bytes12)
	return prefix + string(rune('0'+checkDigit))
}

// Valid reports whether Normalize(s) succeeds.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// Equal reports whether a and b normalise to the same ISBN-13. Two
// strings that both fail to normalise are compared verbatim.
func Equal(a, b string) bool {
	normA, errA := Normalize(a)
	normB, errB := Normalize(b)

	// If both fail to normalize, compare verbatim
	if errA != nil && errB != nil {
		return a == b
	}

	// If only one fails to normalize, they're not equal
	if (errA != nil) != (errB != nil) {
		return false
	}

	// Both normalized successfully, compare normalized values
	return normA == normB
}
