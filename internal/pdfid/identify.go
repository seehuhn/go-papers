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
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/doi"
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

// SearchHit is one candidate reported to tier 3 for a title guess: the
// candidate's DOI and title, the TitleSimilarity score between the guess
// and that title, and enough of the candidate's own bibliographic data -
// the first author's family name and the publication year - for tier 3 to
// corroborate the title match against the document's own extracted text
// (see corroborated). Author is "" and Year is 0 when the search found
// neither for the top hit.
type SearchHit struct {
	DOI, Title string
	Score      float64
	Author     string // first author's family name, "" if unknown
	Year       int    // publication year, 0 if unknown
}

// SearchFunc resolves a title guess to a search candidate.
// cmd_ingest wires this to Crossref; tests use a stub.
type SearchFunc func(titleGuess string) (SearchHit, error)

// ValidateDOIFunc reports whether doiCandidate is a registered DOI. Tier
// 2 uses it to pick the right rung of a prose DOI candidate's trim
// ladder (see Config and tier2); cmd_ingest.go and cmd_fetch.go wire
// this to sources.Handle.Exists, which checks existence independent of
// which registration agency issued the DOI.
type ValidateDOIFunc func(doiCandidate string) (bool, error)

// Config bundles Identify's optional external resolvers. Search backs
// tier 3's title lookup; ValidateDOI backs tier 2's prose DOI candidate
// ladder. Either may be left nil: a nil Search skips tier 3 entirely
// (see tier3); a nil ValidateDOI is treated like a validation error (see
// tier2) - tier 2 cannot confirm anything, so it falls back to the raw
// greedy match rather than silently accepting or rejecting a candidate
// it never actually checked.
type Config struct {
	Search      SearchFunc
	ValidateDOI ValidateDOIFunc
}

// tier3MinScore is the minimum TitleSimilarity score a tier-3 candidate
// must reach before it is even considered - but score alone is not
// sufficient, see corroborated.
//
// This used to be 0.8, and score alone was the whole gate. During a bulk
// ingest, that filed Giles's "Multilevel Monte Carlo path simulation" as
// Giles & Waterhouse's "Multilevel quasi-Monte Carlo path simulation": 5
// shared title tokens out of 6 scores 0.833, which cleared 0.8 on the
// title alone. Raising the bar to 0.9 and requiring the candidate to be
// corroborated by the document's own text closes that hole. Rejection is
// safe by design here: a candidate tier 3 wrongly refuses just means
// "paper ingest" reports it cannot tell which paper this is and the
// agent supplies -doi, whereas a candidate tier 3 wrongly accepts files
// the wrong paper - the worse failure by far.
const tier3MinScore = 0.9

// tier3MinTokens is the minimum number of tokens a TopLines entry must
// have to be considered a plausible title (shorter lines are running
// heads, "Preprint" stamps, or similar decorations, and are skipped).
const tier3MinTokens = 4

var (
	// arxivNewRegex matches the standard left-margin arXiv stamp used
	// since 2007, e.g. "arXiv:2412.05039v2".
	arxivNewRegex = regexp.MustCompile(`arXiv:(\d{4}\.\d{4,5})(v\d+)?`)

	// arxivOldRegex matches the pre-2007 archive/subject-class style
	// stamp, e.g. "arXiv:math.PR/0605234v1".
	arxivOldRegex = regexp.MustCompile(`arXiv:([a-z-]+(\.[A-Z]{2})?/\d{7})(v\d+)?`)
)

// Identify runs the tiers in order over an extracted document.
// Tier 3 accepts a candidate only when its title similarity clears
// tier3MinScore and the candidate is corroborated by the document's own
// text (see corroborated); otherwise ID.Tier is 0 and Title still carries
// the guess so the error message can show it. See Config for the
// external resolvers tiers 2 and 3 use.
func Identify(d *DocText, cfg Config) ID {
	scanned := allPagesEmpty(d.Pages)

	if id, ok := tier1(d); ok {
		id.Scanned = scanned
		return id
	}
	if id, ok := tier2(d, cfg.ValidateDOI); ok {
		id.Scanned = scanned
		return id
	}

	id := tier3(d, cfg.Search)
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

// tier1Values collects the metadata strings that tier 1 regex-scans:
// Subject and Keywords (each included only if set), followed by the
// Custom map's values in sorted-by-key order (for determinism), and
// finally the text of the XMP packet. Within this list an Info-dict
// entry wins over whatever a producer left in the packet, which is why
// the packet text comes last.
//
// The list is only reached when the packet stated no DOI: [tier1] takes
// DocText.XMPDOI first, so for DOIs an XMP property whose meaning is
// "this is the DOI" outranks every entry here. The ordering below
// governs the remaining cases - a DOI nobody stated typed, and the arXiv
// stamp scan, which has no typed path at all.
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

// tier2 looks for a DOI or arXiv stamp in the extracted page text. DOI
// is tried first, across all pages (see tier2DOI); only if no page
// yields one are the pages checked again for an arXiv stamp. DOI is
// preferred over arXiv ID when both could in principle appear, since a
// DOI more precisely identifies the published version of record.
func tier2(d *DocText, validate ValidateDOIFunc) (ID, bool) {
	if id, ok := tier2DOI(d, validate); ok {
		return id, true
	}
	for _, page := range d.Pages {
		if id, version, ok := findArxiv(page); ok {
			return ID{ArxivID: id, Version: version, Tier: 2}, true
		}
	}
	return ID{}, false
}

// tier2DOI scans d.Pages, in order, for a DOI. Unlike tier 1's metadata
// scan (see findDOI), a page-text DOI is a guess that must be checked
// against validate before it is trusted - prose can glue trailing
// punctuation onto a real DOI, or wrap one in a parenthetical, so the
// raw greedy match is not automatically the right string.
//
// Within a page, doi.Candidates yields that page's DOI-shaped matches as
// a longest-first trim ladder per match, matches in the order found.
// Each candidate is validated in turn, memoised so the same candidate
// string is never checked twice in this call (validateCached):
//
//   - a confirmed hit wins outright, immediately;
//   - a rejected candidate moves on to the next rung (or match);
//   - a validation error - or no validator at all - abandons validation
//     for the rest of this call and falls back to the page's first (raw,
//     untrimmed) candidate, unvalidated. This preserves the old regex
//     scan's behaviour of just taking whatever it found rather than
//     losing the DOI to a transient network failure; see ValidateDOIFunc
//     error semantics require the caller to have wired a working
//     validator to get validated results at all.
//
// Only once every page's candidates are exhausted without a hit or an
// error does tier2DOI report failure, so tier2 can fall through to the
// arXiv stamp scan.
func tier2DOI(d *DocText, validate ValidateDOIFunc) (ID, bool) {
	cache := map[string]validation{}
	for _, page := range d.Pages {
		cands := doi.Candidates(page)
		if len(cands) == 0 {
			continue
		}
		for _, c := range cands {
			ok, err := validateCached(c, validate, cache)
			if err != nil {
				return ID{DOI: cands[0], Tier: 2}, true
			}
			if ok {
				return ID{DOI: c, Tier: 2}, true
			}
		}
	}
	return ID{}, false
}

// errNoValidator stands in for a validation error when no ValidateDOIFunc
// was wired at all: tier2DOI has no way to confirm a candidate, so it
// takes the same "stop and fall back to the raw match" path as a real
// validator error.
var errNoValidator = errors.New("pdfid: no DOI validator configured")

// validation is one memoised validateCached result.
type validation struct {
	ok  bool
	err error
}

// validateCached calls validate(c), memoising the result in cache so
// that a candidate seen again - on a later page, or a later rung of the
// same ladder - is not re-validated.
func validateCached(c string, validate ValidateDOIFunc, cache map[string]validation) (bool, error) {
	if v, seen := cache[c]; seen {
		return v.ok, v.err
	}
	var v validation
	if validate == nil {
		v.err = errNoValidator
	} else {
		v.ok, v.err = validate(c)
	}
	cache[c] = v
	return v.ok, v.err
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

	hit, err := search(guess)
	if err != nil {
		return ID{Title: guess}
	}
	if hit.Score >= tier3MinScore && corroborated(hit, d) {
		return ID{DOI: hit.DOI, Title: guess, Tier: 3}
	}
	return ID{Title: guess}
}

// corroborated reports whether hit is corroborated by the document's own
// extracted text (d.Pages): hit.Author's folded family name occurs among
// the folded tokens of that text, or hit.Year appears there as a token.
// A hit with neither an author nor a year is never corroborated - title
// similarity alone is not enough, see tier3MinScore.
func corroborated(hit SearchHit, d *DocText) bool {
	if hit.Author == "" && hit.Year == 0 {
		return false
	}

	tokens := make(map[string]bool)
	for _, t := range match.Tokens(strings.Join(d.Pages, "\n")) {
		tokens[t] = true
	}

	if authorTokens := match.Tokens(hit.Author); len(authorTokens) > 0 {
		found := true
		for _, t := range authorTokens {
			if !tokens[t] {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}

	return hit.Year != 0 && tokens[strconv.Itoa(hit.Year)]
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

// findDOI returns tier 1's DOI guess from s: the first, longest
// (untrimmed) rung of doi.Candidates(s), if any. Tier 1 trusts document
// metadata and does not validate this guess over the network - see the
// package doc on Config.ValidateDOI for the tier that does, tier 2's
// prose scan (tier2DOI), which needs the whole trim ladder rather than
// just its first rung.
func findDOI(s string) (string, bool) {
	cands := doi.Candidates(s)
	if len(cands) == 0 {
		return "", false
	}
	return cands[0], true
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
