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

package pdfid

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/match"
)

// ID is the outcome of identifying one PDF.
type ID struct {
	DOI     string
	ArxivID string
	Version int    // arXiv version when the stamp carries one
	Title   string // tier-3 title guess (unicode), "" otherwise
	Tier    int    // 1 = metadata, 2 = text regex, 3 = title guess, 0 = unidentified
	Scanned bool   // true when no page yielded text
}

// SearchFunc resolves a title guess to (DOI, matchedTitle, score, err).
// cmd_ingest wires this to Crossref; tests use a stub. score is the
// TitleSimilarity between guess and the hit's title.
type SearchFunc func(titleGuess string) (doi, matchedTitle string, score float64, err error)

// tier3MinScore is the minimum TitleSimilarity score required for a tier-3
// title guess to be accepted as an identification.
const tier3MinScore = 0.8

// tier3MinTokens is the minimum number of tokens a TopLines entry must
// have to be considered a plausible title (shorter lines are running
// heads, "Preprint" stamps, or similar decorations, and are skipped).
const tier3MinTokens = 4

var (
	// doiRegex matches a bare DOI. Trailing punctuation that is not part
	// of the DOI itself (".", ",", ";", ")") is trimmed from the match
	// separately, since the character class below cannot distinguish
	// DOI-internal punctuation from a sentence's trailing full stop.
	doiRegex = regexp.MustCompile(`10\.\d{4,9}/[^\s"<>]+`)

	// arxivNewRegex matches the standard left-margin arXiv stamp used
	// since 2007, e.g. "arXiv:2412.05039v2".
	arxivNewRegex = regexp.MustCompile(`arXiv:(\d{4}\.\d{4,5})(v\d+)?`)

	// arxivOldRegex matches the pre-2007 archive/subject-class style
	// stamp, e.g. "arXiv:math.PR/0605234v1".
	arxivOldRegex = regexp.MustCompile(`arXiv:([a-z-]+(\.[A-Z]{2})?/\d{7})(v\d+)?`)
)

// Identify runs the tiers in order over an extracted document.
// Tier 3 accepts only score >= 0.8; below that ID.Tier is 0 and Title
// still carries the guess so the error message can show it.
func Identify(d *DocText, search SearchFunc) ID {
	scanned := allPagesEmpty(d.Pages)

	if id, ok := tier1(d); ok {
		id.Scanned = scanned
		return id
	}
	if id, ok := tier2(d); ok {
		id.Scanned = scanned
		return id
	}

	id := tier3(d, search)
	id.Scanned = scanned
	return id
}

// allPagesEmpty reports whether every page text is empty after trimming,
// which is how a scanned (text-free) PDF is detected.
func allPagesEmpty(pages []string) bool {
	for _, p := range pages {
		if strings.TrimSpace(p) != "" {
			return false
		}
	}
	return true
}

// tier1 looks for a DOI or arXiv stamp in the document metadata: the
// XMP packet's typed DOI properties, the Info dictionary's Subject and
// Keywords fields, its non-standard entries (Custom), where a "doi"
// entry often hides, and the text of the XMP packet. The candidate
// strings are scanned in a deterministic order (see tier1Values): all of
// them are checked for a DOI first, then, only if no DOI was found
// anywhere, all of them are checked for an arXiv stamp. Title is not
// scanned (per the task brief, "Title unused here").
//
// DocText.XMPDOI comes before everything else because it is not a
// pattern match at all: it is a DOI the producer stated in a property
// whose meaning is "this is the DOI" (prism:doi, pdfx:doi,
// dc:identifier), read as a typed value and normalized. Every other
// candidate is a regex guess over free text, which can both miss (a
// SICI DOI is truncated at its first '<') and mislead (a reference list
// in Keywords). A stated DOI outranks a guessed one.
func tier1(d *DocText) (ID, bool) {
	if d.XMPDOI != "" {
		return ID{DOI: d.XMPDOI, Tier: 1}, true
	}

	values := tier1Values(d)
	if len(values) == 0 {
		return ID{}, false
	}

	for _, v := range values {
		if doi, ok := findDOI(v); ok {
			return ID{DOI: doi, Tier: 1}, true
		}
	}
	for _, v := range values {
		if id, version, ok := findArxiv(v); ok {
			return ID{ArxivID: id, Version: version, Tier: 1}, true
		}
	}
	return ID{}, false
}

// tier1Values collects the metadata strings that tier 1 scans: Subject
// and Keywords (each included only if set), followed by the Custom map's
// values in sorted-by-key order (for determinism), and finally the
// serialised XMP packet. The XMP packet comes last so that an explicit
// Info-dict entry wins over whatever a producer left in the packet.
func tier1Values(d *DocText) []string {
	var values []string
	if d.Subject != "" {
		values = append(values, d.Subject)
	}
	if d.Keywords != "" {
		values = append(values, d.Keywords)
	}
	for _, k := range slices.Sorted(maps.Keys(d.Custom)) {
		values = append(values, d.Custom[k])
	}
	if d.XMP != "" {
		values = append(values, d.XMP)
	}
	return values
}

// tier2 looks for a DOI or arXiv stamp in the extracted page text. All
// pages are checked for a DOI first (in page order); only if no page
// carries a DOI are the pages checked again for an arXiv stamp. DOI is
// preferred over arXiv ID when both could in principle appear, since a
// DOI more precisely identifies the published version of record.
func tier2(d *DocText) (ID, bool) {
	for _, page := range d.Pages {
		if doi, ok := findDOI(page); ok {
			return ID{DOI: doi, Tier: 2}, true
		}
	}
	for _, page := range d.Pages {
		if id, version, ok := findArxiv(page); ok {
			return ID{ArxivID: id, Version: version, Tier: 2}, true
		}
	}
	return ID{}, false
}

// tier3 guesses the title from the first page's largest-font text and, if
// a SearchFunc is provided, resolves it against an external search. The
// guess is reported in ID.Title whenever one was attempted (even when the
// search fails or scores too low), so that callers can show it in error
// messages. A nil search skips tier 3 entirely.
//
// When the page text yields no usable guess - a scanned PDF, or a first
// page whose lines are all too short - the document metadata is used as
// the title source instead: first the Info dictionary's Title, then the
// XMP dc:title. Such a guess passes through the same similarity gate as
// a page-text guess.
func tier3(d *DocText, search SearchFunc) ID {
	if search == nil {
		return ID{}
	}

	guess := titleGuess(d.TopLines, d.TopSizes)
	if guess == "" {
		guess = metadataTitle(d)
	}
	if guess == "" {
		return ID{}
	}

	doi, _, score, err := search(guess)
	if err != nil {
		return ID{Title: guess}
	}
	if score >= tier3MinScore {
		return ID{DOI: doi, Title: guess, Tier: 3}
	}
	return ID{Title: guess}
}

// metadataTitle returns the tier-3 fallback title guess taken from the
// document metadata: the Info dictionary's Title if set, otherwise the
// XMP dc:title. It returns "" if neither is set.
func metadataTitle(d *DocText) string {
	if title := strings.TrimSpace(d.Title); title != "" {
		return title
	}
	return strings.TrimSpace(d.XMPTitle)
}

// titleGuess picks the tier-3 title guess from topLines, which holds the
// first page's lines ordered by descending font size (largest first), and
// topSizes, the parallel slice of font sizes (topSizes[i] is the font
// size of topLines[i]; see DocText.TopSizes).
//
// The leading run of topLines entries that share the largest font size
// (topSizes[0]) is joined with spaces into one candidate, since a
// wrapped title commonly spans two or more same-size lines. If that
// candidate has at least tier3MinTokens tokens, it is the guess. If it is
// too short (e.g. the largest-font run is just a running head or a
// decorative mark), the remaining lines are checked individually, in
// font-size order, and the first one with at least tier3MinTokens tokens
// is returned.
//
// If topSizes does not have the same length as topLines - which callers
// constructing a DocText by hand, such as tests, may leave unset - no
// join is attempted and this degrades to considering topLines[0] alone,
// then falling through line by line exactly as before TopSizes existed.
func titleGuess(topLines []string, topSizes []float64) string {
	if len(topLines) == 0 {
		return ""
	}

	sameSize := len(topSizes) == len(topLines)
	end := 1
	for end < len(topLines) && sameSize && topSizes[end] == topSizes[0] {
		end++
	}

	joined := strings.Join(topLines[:end], " ")
	if len(match.Tokens(joined)) >= tier3MinTokens {
		return joined
	}

	for _, line := range topLines[end:] {
		if len(match.Tokens(line)) >= tier3MinTokens {
			return line
		}
	}
	return ""
}

// findDOI returns the first DOI in s, if any, with trailing punctuation
// that is not part of the DOI (".", ",", ";", ")") trimmed off.
func findDOI(s string) (string, bool) {
	m := doiRegex.FindString(s)
	if m == "" {
		return "", false
	}
	return strings.TrimRight(m, ".,;)"), true
}

// findArxiv returns the arXiv ID and version (0 if unversioned) of the
// first arXiv stamp in s, if any. Both the modern (YYMM.NNNNN) and the
// old-style (archive/subject-class/YYMMNNN) stamp forms are recognised;
// old-style IDs keep their slash, unchanged, in the returned ID.
func findArxiv(s string) (id string, version int, ok bool) {
	if m := arxivNewRegex.FindStringSubmatch(s); m != nil {
		return m[1], parseVersion(m[2]), true
	}
	if m := arxivOldRegex.FindStringSubmatch(s); m != nil {
		return m[1], parseVersion(m[3]), true
	}
	return "", 0, false
}

// parseVersion parses a "vN" version suffix (as captured by the arXiv
// regexes) into N, returning 0 for an empty or unparsable suffix.
func parseVersion(v string) int {
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(v, "v"))
	if err != nil {
		return 0
	}
	return n
}
