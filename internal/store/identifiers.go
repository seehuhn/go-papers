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
	"strings"

	isbnlib "seehuhn.de/go/paper/internal/isbn"
)

// arxivDOIPrefix is the registrant+suffix prefix arXiv uses for the DOIs
// it issues itself (e.g. "10.48550/arXiv.2412.05039"), matched
// case-insensitively.
const arxivDOIPrefix = "10.48550/arxiv."

// IsArxivDOI reports whether d is an arXiv-issued DOI (10.48550/arXiv.…),
// which stands in for a journal DOI until one is known.
func IsArxivDOI(d string) bool {
	return strings.HasPrefix(strings.ToLower(d), arxivDOIPrefix)
}

// RecordIdentifiers writes doi and isbn onto p where p lacks them. An
// empty argument is "unknown" and changes nothing. A value equal to what
// p holds (DOI case-insensitively, ISBN by isbn.Equal) changes nothing.
// An arXiv-issued DOI on p is replaced by a non-arXiv doi. Any other
// mismatch is an error naming both values and nothing is written. ISBNs
// are stored normalised.
//
// It returns one log detail per change ("recorded doi 10.…", "replaced
// arXiv DOI 10.48550/… by 10.…", "recorded isbn 978…"); the caller
// appends the LogEntry with its own timestamp and action.
func (p *Paper) RecordIdentifiers(doi, isbn string) ([]string, error) {
	var details []string

	newDOI := p.DOI
	if doi != "" {
		switch {
		case p.DOI == "":
			newDOI = doi
			details = append(details, fmt.Sprintf("recorded doi %s", doi))
		case strings.EqualFold(p.DOI, doi):
			// already recorded: no change
		case IsArxivDOI(p.DOI):
			details = append(details, fmt.Sprintf("replaced arXiv DOI %s by %s", p.DOI, doi))
			newDOI = doi
		default:
			return nil, fmt.Errorf("doi: %s does not match already-recorded doi %s", doi, p.DOI)
		}
	}

	newISBN := p.ISBN
	if isbn != "" {
		norm, err := isbnlib.Normalize(isbn)
		if err != nil {
			return nil, fmt.Errorf("isbn: %q is not a valid ISBN-10 or ISBN-13 (checksum or length)", isbn)
		}
		switch {
		case p.ISBN == "":
			newISBN = norm
			details = append(details, fmt.Sprintf("recorded isbn %s", norm))
		case isbnlib.Equal(p.ISBN, isbn):
			// already recorded: no change
		default:
			return nil, fmt.Errorf("isbn: %s does not match already-recorded isbn %s", isbn, p.ISBN)
		}
	}

	p.DOI = newDOI
	p.ISBN = newISBN
	return details, nil
}
