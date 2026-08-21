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
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ArxivEntry is the metadata for one arXiv record, as returned by the
// arXiv API.
type ArxivEntry struct {
	ID           string // "2412.05039" (no version)
	Version      int    // from the entry id URL, e.g. ...v2
	Title        string
	Authors      []string // plain unicode names in natural order ("Jochen Voß")
	Abstract     string
	DOI          string // when the record links a published version
	JournalRef   string // free-text journal reference, when present
	Year         int    // from <published>
	PrimaryClass string // e.g. "math.PR"
}

// Arxiv is a client for the arXiv API (https://export.arxiv.org).
type Arxiv struct {
	BaseURL string // default "https://export.arxiv.org"
	Client  *http.Client
}

// baseURL returns a.BaseURL, defaulting to the public arXiv API.
func (a *Arxiv) baseURL() string {
	if a.BaseURL == "" {
		return "https://export.arxiv.org"
	}
	return a.BaseURL
}

// arxivFeed is the top-level Atom feed returned by the arXiv API.
type arxivFeed struct {
	Entries []arxivFeedEntry `xml:"entry"`
}

// arxivFeedEntry is one Atom entry in an arXiv API response.
type arxivFeedEntry struct {
	ID              string             `xml:"id"`
	Title           string             `xml:"title"`
	Summary         string             `xml:"summary"`
	Published       string             `xml:"published"`
	Authors         []arxivFeedAuthor  `xml:"author"`
	DOI             string             `xml:"doi"`
	JournalRef      string             `xml:"journal_ref"`
	PrimaryCategory arxivFeedPrimClass `xml:"primary_category"`
}

// arxivFeedAuthor is one <author> element in an arXiv API response.
type arxivFeedAuthor struct {
	Name string `xml:"name"`
}

// arxivFeedPrimClass is the <arxiv:primary_category> element, whose value
// is carried in the term attribute.
type arxivFeedPrimClass struct {
	Term string `xml:"term,attr"`
}

// arxivIDRe extracts the arXiv ID and, when present, the version from the
// URL found in an entry's <id> element, e.g.
// "http://arxiv.org/abs/2412.05039v2" or the old-style
// "http://arxiv.org/abs/math.PR/0605234v1". Matching starts right after
// "/abs/" and is unrestricted in what it can consume, so (unlike a
// [^/]-restricted pattern) it correctly keeps the category prefix and
// slash used by pre-2007 IDs as part of the ID.
var arxivIDRe = regexp.MustCompile(`/abs/(.+?)(?:v(\d+))?$`)

// ByID fetches the record for one arXiv ID (with or without version
// suffix). An unknown ID returns an error wrapping ErrNotFound.
func (a *Arxiv) ByID(id string) (*ArxivEntry, error) {
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	u := a.baseURL() + "/api/query?id_list=" + id

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("arxiv: fetching %s: %w", id, err)
	}
	req.Header.Set("User-Agent", UserAgent(""))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv: fetching %s: %w", id, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("arxiv: fetching %s: %w", id, err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("arxiv: unexpected status %s for %s: %s", resp.Status, u, snippet)
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("arxiv: decoding response for %s: %w", id, err)
	}

	if len(feed.Entries) == 0 {
		return nil, fmt.Errorf("arxiv: %s: %w", id, ErrNotFound)
	}
	// id_list carries exactly one ID, so the query returns at most one entry.
	entry := feed.Entries[0]

	arxivID, version := parseArxivID(entry.ID)

	authors := make([]string, len(entry.Authors))
	for i, au := range entry.Authors {
		authors[i] = normalizeWhitespace(au.Name)
	}

	year := 0
	if len(entry.Published) >= 4 {
		if y, err := strconv.Atoi(entry.Published[:4]); err == nil {
			year = y
		}
	}

	return &ArxivEntry{
		ID:           arxivID,
		Version:      version,
		Title:        normalizeWhitespace(entry.Title),
		Authors:      authors,
		Abstract:     normalizeWhitespace(entry.Summary),
		DOI:          entry.DOI,
		JournalRef:   entry.JournalRef,
		Year:         year,
		PrimaryClass: entry.PrimaryCategory.Term,
	}, nil
}

// parseArxivID extracts the arXiv ID (without version) and the version
// number from the URL found in an entry's <id> element, e.g.
// "http://arxiv.org/abs/2412.05039v2" -> ("2412.05039", 2), or
// "http://arxiv.org/abs/math.PR/0605234v1" -> ("math.PR/0605234", 1) for
// old-style IDs with a category prefix. When there is no trailing version,
// e.g. "http://arxiv.org/abs/2412.05039", the version is 0.
func parseArxivID(idURL string) (string, int) {
	m := arxivIDRe.FindStringSubmatch(idURL)
	if m == nil {
		return idURL, 0
	}
	version, _ := strconv.Atoi(m[2])
	return m[1], version
}

// whitespaceRe matches runs of whitespace, used to normalize text that
// arXiv wraps across multiple lines.
var whitespaceRe = regexp.MustCompile(`\s+`)

// normalizeWhitespace collapses runs of whitespace to single spaces and
// trims leading and trailing whitespace.
func normalizeWhitespace(s string) string {
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
}

// PDFURL returns the download URL for the PDF of a specific version of an
// arXiv record. base is normally "https://arxiv.org".
func PDFURL(base, id string, version int) string {
	return fmt.Sprintf("%s/pdf/%sv%d", base, id, version)
}

// SourceURL returns the download URL for the source archive of a specific
// version of an arXiv record. base is normally "https://arxiv.org".
func SourceURL(base, id string, version int) string {
	return fmt.Sprintf("%s/e-print/%sv%d", base, id, version)
}
