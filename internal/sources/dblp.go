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
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// dblpAuthor is a single author entry in a DBLP hit.
type dblpAuthor struct {
	Text string `json:"text"`
}

// dblpAuthorList decodes a DBLP "author" member. DBLP serializes this as a
// JSON array when a hit has multiple authors, but collapses it to a bare
// author object when there is exactly one. UnmarshalJSON accepts both
// shapes; a malformed member is tolerated as an empty list rather than
// causing an error, consistent with this client's tolerant-parsing
// contract.
type dblpAuthorList []dblpAuthor

func (l *dblpAuthorList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) == 0, string(data) == "null":
		*l = nil
	case data[0] == '[':
		var arr []dblpAuthor
		if err := json.Unmarshal(data, &arr); err != nil {
			*l = nil
			return nil
		}
		*l = arr
	case data[0] == '{':
		var one dblpAuthor
		if err := json.Unmarshal(data, &one); err != nil {
			*l = nil
			return nil
		}
		*l = dblpAuthorList{one}
	default:
		*l = nil
	}
	return nil
}

// dblpSearchResult is the envelope returned by the DBLP publication search
// API. Only the fields needed to build a Candidate are parsed; all others
// are ignored.
type dblpSearchResult struct {
	Result struct {
		Hits struct {
			Hit []struct {
				Info struct {
					Title   string `json:"title"`
					Venue   string `json:"venue"`
					Year    string `json:"year"`
					DOI     string `json:"doi"`
					Authors struct {
						Author dblpAuthorList `json:"author"`
					} `json:"authors"`
				} `json:"info"`
			} `json:"hit"`
		} `json:"hits"`
	} `json:"result"`
}

// DBLP is a client for the DBLP publication search API
// (https://dblp.org).
type DBLP struct {
	BaseURL string // default "https://dblp.org"
	Client  *http.Client
}

// baseURL returns d.BaseURL, defaulting to the public DBLP API.
func (d *DBLP) baseURL() string {
	if d.BaseURL == "" {
		return "https://dblp.org"
	}
	return d.BaseURL
}

// Search runs a free-text query against DBLP and returns up to limit
// candidates. DBLP is a candidate provider only: results are never
// auto-accepted, so parsing is intentionally tolerant of missing fields. A
// response with no hits at all (the "hit" member absent) returns an empty
// slice and a nil error.
func (d *DBLP) Search(query string, limit int) ([]Candidate, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("h", strconv.Itoa(limit))
	u := d.baseURL() + "/search/publ/api?" + q.Encode()

	var res dblpSearchResult
	if err := getJSON(d.Client, u, httpOptions{}, &res); err != nil {
		return nil, fmt.Errorf("dblp: searching %q: %w", query, err)
	}

	items := res.Result.Hits.Hit
	if len(items) > limit {
		items = items[:limit]
	}

	hits := make([]Candidate, 0, len(items))
	for _, item := range items {
		c := Candidate{
			Source: "dblp",
			Title:  item.Info.Title,
			Venue:  item.Info.Venue,
			DOI:    item.Info.DOI,
		}
		c.Year, _ = strconv.Atoi(item.Info.Year)
		for _, a := range item.Info.Authors.Author {
			c.Authors = append(c.Authors, a.Text)
		}
		hits = append(hits, c)
	}
	return hits, nil
}
