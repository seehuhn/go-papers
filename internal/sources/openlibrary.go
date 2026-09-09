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
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/isbn"
)

// OLWork is the metadata for one work, as returned by the Open Library
// search API.
type OLWork struct {
	Key       string // "/works/OL123W"
	Title     string
	Authors   []string
	FirstYear int
}

// OLEdition is the metadata for one edition of a work, as returned by the
// Open Library editions API.
type OLEdition struct {
	Key         string
	Title       string
	ISBN        string // normalised ISBN-13, "" if none
	Year        int
	Format      string // lower-cased physical_format, "" unknown
	EditionName string
	Publishers  []string
}

// OpenLibrary is a client for the Open Library API
// (https://openlibrary.org).
type OpenLibrary struct {
	BaseURL string // default "https://openlibrary.org"
	Client  *http.Client
}

// baseURL returns o.BaseURL, defaulting to the public Open Library API.
func (o *OpenLibrary) baseURL() string {
	if o.BaseURL == "" {
		return "https://openlibrary.org"
	}
	return o.BaseURL
}

// olSearchResponse is the envelope returned by the Open Library search API.
type olSearchResponse struct {
	Docs []struct {
		Key              string   `json:"key"`
		Title            string   `json:"title"`
		AuthorName       []string `json:"author_name"`
		FirstPublishYear int      `json:"first_publish_year"`
	} `json:"docs"`
}

// Search runs a title/author query against Open Library and returns the
// matching works in result order, up to limit.
func (o *OpenLibrary) Search(title, author string, limit int) ([]OLWork, error) {
	q := url.Values{}
	q.Set("title", title)
	if author != "" {
		q.Set("author", author)
	}
	q.Set("fields", "key,title,author_name,first_publish_year")
	q.Set("limit", strconv.Itoa(limit))
	u := o.baseURL() + "/search.json?" + q.Encode()

	var res olSearchResponse
	if err := getJSON(o.Client, u, httpOptions{}, &res); err != nil {
		return nil, fmt.Errorf("openlibrary: searching %q: %w", title, err)
	}

	works := make([]OLWork, 0, len(res.Docs))
	for _, d := range res.Docs {
		works = append(works, OLWork{
			Key:       d.Key,
			Title:     d.Title,
			Authors:   d.AuthorName,
			FirstYear: d.FirstPublishYear,
		})
	}
	return works, nil
}

// olEditionsResponse is the envelope returned by the Open Library editions
// API.
type olEditionsResponse struct {
	Entries []struct {
		Key            string   `json:"key"`
		Title          string   `json:"title"`
		ISBN13         []string `json:"isbn_13"`
		ISBN10         []string `json:"isbn_10"`
		PublishDate    string   `json:"publish_date"`
		PhysicalFormat string   `json:"physical_format"`
		EditionName    string   `json:"edition_name"`
		Publishers     []string `json:"publishers"`
	} `json:"entries"`
}

// yearRunRE matches a run of 4 digits, used to pull a year out of a
// publish_date string of unpredictable shape ("1994", "March 21, 1995",
// ...).
var yearRunRE = regexp.MustCompile(`\d{4}`)

// parsePublishYear returns the year encoded in an Open Library publish_date
// string: the last run of 4 digits it contains, or 0 when there is none.
func parsePublishYear(s string) int {
	runs := yearRunRE.FindAllString(s, -1)
	if len(runs) == 0 {
		return 0
	}
	y, err := strconv.Atoi(runs[len(runs)-1])
	if err != nil {
		return 0
	}
	return y
}

// editionISBN picks the ISBN to record for an edition: the first isbn_13
// entry that normalises, falling back to the first normalising isbn_10
// entry (converted to ISBN-13). It returns "" when none normalise.
func editionISBN(isbn13, isbn10 []string) string {
	for _, s := range isbn13 {
		if n, err := isbn.Normalize(s); err == nil {
			return n
		}
	}
	for _, s := range isbn10 {
		if n, err := isbn.Normalize(s); err == nil {
			return n
		}
	}
	return ""
}

// Editions fetches every edition of the work at workKey (e.g.
// "/works/OL123W").
func (o *OpenLibrary) Editions(workKey string) ([]OLEdition, error) {
	u := o.baseURL() + workKey + "/editions.json?limit=100"

	var res olEditionsResponse
	if err := getJSON(o.Client, u, httpOptions{}, &res); err != nil {
		return nil, fmt.Errorf("openlibrary: fetching editions of %s: %w", workKey, err)
	}

	editions := make([]OLEdition, 0, len(res.Entries))
	for _, e := range res.Entries {
		editions = append(editions, OLEdition{
			Key:         e.Key,
			Title:       e.Title,
			ISBN:        editionISBN(e.ISBN13, e.ISBN10),
			Year:        parsePublishYear(e.PublishDate),
			Format:      strings.ToLower(e.PhysicalFormat),
			EditionName: e.EditionName,
			Publishers:  e.Publishers,
		})
	}
	return editions, nil
}

// ordinalDigitRE extracts the leading digit run of a bibtex edition string
// (e.g. "2nd" -> "2").
var ordinalDigitRE = regexp.MustCompile(`\d+`)

// ordinalDigit returns the digit run in edition, or "" when edition carries
// none (including when edition is "").
func ordinalDigit(edition string) string {
	return ordinalDigitRE.FindString(edition)
}

// formatRank ranks a lower-cased physical_format for PickEdition's
// tie-break: hardcover, then paperback, then anything else (including
// unknown), then electronic editions last.
func formatRank(format string) int {
	switch format {
	case "hardcover":
		return 0
	case "paperback":
		return 1
	case "electronic resource", "ebook", "kindle":
		return 3
	default:
		return 2
	}
}

// PickEdition chooses the edition to record for a work described by bibtex
// edition text (e.g. "2nd", "") and year (0 unknown). Only editions with an
// ISBN qualify; editions without one are dropped entirely, from both pick
// and others. Qualifying editions are ranked by: whether EditionName
// contains the same ordinal digit as edition; then Year == year; then
// Format hardcover > paperback > other/unknown > e-book ("electronic
// resource", "ebook", "kindle"); then the earliest Year, with an unknown
// Year (0) last rather than first; then the smallest ISBN. pick is the
// best-ranked edition; others are the rest in that same ranked order
// (next-best first), so a caller can print them as ranked alternatives.
// ok is false when nothing qualifies.
func PickEdition(editions []OLEdition, edition string, year int) (pick OLEdition, others []OLEdition, ok bool) {
	var qualifying []OLEdition
	for _, e := range editions {
		if e.ISBN != "" {
			qualifying = append(qualifying, e)
		}
	}
	if len(qualifying) == 0 {
		return OLEdition{}, nil, false
	}

	digit := ordinalDigit(edition)
	// digitScore ranks EditionName containing digit above those that
	// don't. strings.Contains(s, "") is always true, so an empty digit (no
	// ordinal in edition) makes this tier a no-op: every edition scores 0.
	digitScore := func(e OLEdition) int {
		if strings.Contains(e.EditionName, digit) {
			return 0
		}
		return 1
	}
	// yearScore ranks Year == year above the rest, but only when year is
	// known; year == 0 makes this tier a no-op so an unknown target year
	// never favours editions whose own year failed to parse.
	yearScore := func(e OLEdition) int {
		if year == 0 || e.Year == year {
			return 0
		}
		return 1
	}

	sort.SliceStable(qualifying, func(i, j int) bool {
		a, b := qualifying[i], qualifying[j]
		if da, db := digitScore(a), digitScore(b); da != db {
			return da < db
		}
		if ya, yb := yearScore(a), yearScore(b); ya != yb {
			return ya < yb
		}
		if fa, fb := formatRank(a.Format), formatRank(b.Format); fa != fb {
			return fa < fb
		}
		// The earliest year wins, but a year of 0 means "not parsed",
		// not "very early", so such an edition sorts last.
		if a.Year != b.Year {
			switch {
			case a.Year == 0:
				return false
			case b.Year == 0:
				return true
			}
			return a.Year < b.Year
		}
		return a.ISBN < b.ISBN
	})

	return qualifying[0], qualifying[1:], true
}
