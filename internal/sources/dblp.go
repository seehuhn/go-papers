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
	"encoding/json/jsontext"
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

// dblpVenue decodes a DBLP "venue" member. DBLP normally serializes this as
// a bare string, but cross-listed venues are sometimes serialized as an
// array of strings; the first entry is used in that case. An unrecognized
// shape, or a decode error in either branch, is tolerated as an empty
// string rather than causing an error.
type dblpVenue string

func (v *dblpVenue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) == 0, string(data) == "null":
		*v = ""
	case data[0] == '[':
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil || len(arr) == 0 {
			*v = ""
			return nil
		}
		*v = dblpVenue(arr[0])
	case data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			*v = ""
			return nil
		}
		*v = dblpVenue(s)
	default:
		*v = ""
	}
	return nil
}

// dblpSearchResult is the envelope returned by the DBLP publication search
// API. Each hit is kept as raw JSON so that one malformed hit cannot abort
// the decode of the whole response; see Search.
type dblpSearchResult struct {
	Result struct {
		Hits struct {
			Hit []jsontext.Value `json:"hit"`
		} `json:"hits"`
	} `json:"result"`
}

// dblpHit is the metadata for one DBLP search hit. Only the fields needed
// to build a Candidate are parsed; all others are ignored.
type dblpHit struct {
	Info struct {
		Title   string    `json:"title"`
		Venue   dblpVenue `json:"venue"`
		Year    string    `json:"year"`
		DOI     string    `json:"doi"`
		Authors struct {
			Author dblpAuthorList `json:"author"`
		} `json:"authors"`
	} `json:"info"`
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

	if limit < 0 {
		limit = 0
	}
	raw := res.Result.Hits.Hit
	if len(raw) > limit {
		raw = raw[:limit]
	}

	hits := make([]Candidate, 0, len(raw))
	for _, r := range raw {
		var item dblpHit
		if err := json.Unmarshal(r, &item); err != nil {
			// One malformed hit must not sink the rest of the response.
			continue
		}

		c := Candidate{
			Source: "dblp",
			Title:  item.Info.Title,
			Venue:  string(item.Info.Venue),
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
