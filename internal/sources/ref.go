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

package sources

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/doi"
)

type RefKind int

const (
	RefDOI RefKind = iota + 1
	RefArxiv
	RefText
	// RefPDFURL is a bare http(s) URL that neither the DOI nor the arXiv
	// extractor claimed. It names a file rather than a work: nothing is
	// known about which paper it holds until the bytes are in hand.
	RefPDFURL
)

type Ref struct {
	Kind    RefKind
	DOI     string // set for RefDOI
	ArxivID string // set for RefArxiv, without version suffix
	Version int    // arXiv version, 0 if unspecified
	Text    string // set for RefText
	URL     string // set for RefPDFURL
}

// arxivNewPattern matches the post-2007 arXiv identifier style, e.g.
// "0706.0001".
var arxivNewPattern = regexp.MustCompile(`^\d{4}\.\d{4,5}$`)

// arxivOldPattern matches the pre-2007 arXiv identifier style, e.g.
// "math.PR/0611001" or "hep-th/9901001".
var arxivOldPattern = regexp.MustCompile(`^[a-z-]+(\.[A-Z]{2})?/\d{7}$`)

// ParseRef classifies a reference string as DOI, arXiv ID, or free text.
func ParseRef(s string) Ref {
	s = strings.TrimSpace(s)

	// Try to extract and validate DOI
	if doi := extractDOI(s); doi != "" {
		return Ref{Kind: RefDOI, DOI: doi}
	}

	// Try to extract and validate arXiv ID
	if arxiv := extractArxiv(s); arxiv.ArxivID != "" {
		return arxiv
	}

	// An http(s) URL that survived both extractors names a file, not a
	// work. Whether it really holds a PDF is settled by the download,
	// which checks the %PDF- magic, not by the spelling of the URL.
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return Ref{Kind: RefPDFURL, URL: s}
	}

	// Default to free text
	return Ref{Kind: RefText, Text: s}
}

// doiURLPrefixes lists the doi.org URL forms extractDOI recognises, in
// the order tried. Each is followed by a URL-path-escaped DOI, which is
// unescaped before validation - a doi.org URL for a legacy SICI DOI
// percent-encodes '<'/'>' as %3C/%3E, and the DOI itself must come back
// with the raw characters, not the escapes.
var doiURLPrefixes = []string{
	"https://doi.org/",
	"http://doi.org/",
	"https://dx.doi.org/",
	"http://dx.doi.org/",
	"doi.org/",
}

// extractDOI tries to extract a DOI from the input string: a bare DOI, a
// "doi:"-prefixed value, or a doi.org URL (see doiURLPrefixes). The
// candidate is taken whole and validated with doi.Syntactic - no
// trimming - since a stated reference is either the DOI or it isn't.
func extractDOI(s string) string {
	if len(s) >= 4 && strings.EqualFold(s[:4], "doi:") {
		return syntacticOrEmpty(strings.TrimSpace(s[4:]))
	}

	for _, prefix := range doiURLPrefixes {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		rest := strings.TrimPrefix(s, prefix)
		if unescaped, err := url.PathUnescape(rest); err == nil {
			rest = unescaped
		}
		return syntacticOrEmpty(rest)
	}

	return syntacticOrEmpty(s)
}

// syntacticOrEmpty returns s if it is a syntactically well-formed DOI,
// "" otherwise.
func syntacticOrEmpty(s string) string {
	if doi.Syntactic(s) {
		return s
	}
	return ""
}

// extractArxiv tries to extract an arXiv ID from the input string.
// Returns a Ref with ArxivID set if successful, empty otherwise.
func extractArxiv(s string) Ref {
	// Remove known arXiv prefixes
	if strings.HasPrefix(s, "https://arxiv.org/abs/") {
		s = strings.TrimPrefix(s, "https://arxiv.org/abs/")
	} else if strings.HasPrefix(s, "http://arxiv.org/abs/") {
		s = strings.TrimPrefix(s, "http://arxiv.org/abs/")
	} else if strings.HasPrefix(s, "https://arxiv.org/pdf/") {
		s = strings.TrimPrefix(s, "https://arxiv.org/pdf/")
		// Remove trailing .pdf if present
		s = strings.TrimSuffix(s, ".pdf")
	} else if strings.HasPrefix(s, "http://arxiv.org/pdf/") {
		s = strings.TrimPrefix(s, "http://arxiv.org/pdf/")
		// Remove trailing .pdf if present
		s = strings.TrimSuffix(s, ".pdf")
	} else if len(s) >= 6 && strings.EqualFold(s[:6], "arxiv:") {
		s = s[6:]
	}

	s = strings.TrimSpace(s)

	// Extract version suffix if present
	version := 0
	var id string

	// Check for version suffix pattern (vN)
	if idx := strings.LastIndex(s, "v"); idx > 0 {
		potential := s[idx+1:]
		if v, err := strconv.Atoi(potential); err == nil && len(potential) > 0 {
			// Valid version suffix found
			id = s[:idx]
			version = v
		} else {
			id = s
		}
	} else {
		id = s
	}

	// Validate arXiv pattern (new or old style)
	if arxivNewPattern.MatchString(id) || arxivOldPattern.MatchString(id) {
		return Ref{Kind: RefArxiv, ArxivID: id, Version: version}
	}

	return Ref{}
}
