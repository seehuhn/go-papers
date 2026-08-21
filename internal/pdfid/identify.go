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

// tier1 looks for a DOI or arXiv stamp in the Info dictionary's
// non-standard entries (Custom). The PDF's standard Subject and Keywords
// fields are natural homes for such identifiers, but DocText (as produced
// by Extract) does not carry them as separate fields; Extract only ever
// clones the Custom map, so any such value that survives extraction
// arrives there. Custom's values are scanned in a deterministic (sorted
// by key) order: all values are checked for a DOI first, then, only if
// no DOI was found anywhere, all values are checked for an arXiv stamp.
func tier1(d *DocText) (ID, bool) {
	if len(d.Custom) == 0 {
		return ID{}, false
	}
	keys := slices.Sorted(maps.Keys(d.Custom))

	for _, k := range keys {
		if doi, ok := findDOI(d.Custom[k]); ok {
			return ID{DOI: doi, Tier: 1}, true
		}
	}
	for _, k := range keys {
		if id, version, ok := findArxiv(d.Custom[k]); ok {
			return ID{ArxivID: id, Version: version, Tier: 1}, true
		}
	}
	return ID{}, false
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
func tier3(d *DocText, search SearchFunc) ID {
	if search == nil {
		return ID{}
	}

	guess := titleGuess(d.TopLines)
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

// titleGuess picks the tier-3 title guess from topLines, which holds the
// first page's lines ordered by descending font size (largest first).
//
// The ideal rule (per the task brief) is to take the largest-font
// heading, skipping lines too short to be a title (fewer than
// tier3MinTokens tokens), and to join multiple consecutive lines that
// belong to that same largest-font run into one guess. That last part
// cannot be implemented faithfully here: DocText.TopLines is a []string
// carrying only line text, not the font sizes used to sort it, so once a
// line has been placed in the slice there is no way to tell whether the
// next entry shares its font size or belongs to the next-largest run.
// Reading the brief in the simplest way its actual inputs support: this
// returns the first line with at least tier3MinTokens tokens verbatim,
// without attempting to join it to any neighbouring line.
func titleGuess(topLines []string) string {
	for _, line := range topLines {
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
