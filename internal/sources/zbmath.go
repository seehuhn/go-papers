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
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// zbmathSearchResult is the envelope returned by the zbMATH Open REST API
// search endpoint. Each result item is kept as raw JSON so that one
// malformed hit cannot abort the decode of the whole response; see Search.
type zbmathSearchResult struct {
	Result []jsontext.Value `json:"result"`
}

// zbmathHit is the metadata for one zbMATH search hit. Only the fields
// needed to build a Candidate are parsed; all others are ignored.
type zbmathHit struct {
	Title struct {
		Title string `json:"title"`
	} `json:"title"`
	Contributors struct {
		Authors []struct {
			Name string `json:"name"`
		} `json:"authors"`
	} `json:"contributors"`
	Source struct {
		Series []struct {
			Title string `json:"title"`
		} `json:"series"`
	} `json:"source"`
	Year  int `json:"year"`
	Links []struct {
		Type       string `json:"type"`
		Identifier string `json:"identifier"`
	} `json:"links"`
}

// ZbMath is a client for the zbMATH Open REST API
// (https://api.zbmath.org).
type ZbMath struct {
	BaseURL string // default "https://api.zbmath.org"
	Client  *http.Client
}

// baseURL returns z.BaseURL, defaulting to the public zbMATH Open API.
func (z *ZbMath) baseURL() string {
	if z.BaseURL == "" {
		return "https://api.zbmath.org"
	}
	return z.BaseURL
}

// Search runs a free-text query against zbMATH Open and returns up to limit
// candidates. zbMATH is a candidate provider only: results are never
// auto-accepted, so parsing is intentionally tolerant of missing fields.
func (z *ZbMath) Search(query string, limit int) ([]Candidate, error) {
	q := url.Values{}
	q.Set("search_string", query)
	q.Set("results_per_page", strconv.Itoa(limit))
	u := z.baseURL() + "/v1/document/_search?" + q.Encode()

	var res zbmathSearchResult
	if err := getJSON(z.Client, u, httpOptions{}, &res); err != nil {
		return nil, fmt.Errorf("zbmath: searching %q: %w", query, err)
	}

	if limit < 0 {
		limit = 0
	}
	raw := res.Result
	if len(raw) > limit {
		raw = raw[:limit]
	}

	hits := make([]Candidate, 0, len(raw))
	for _, r := range raw {
		var item zbmathHit
		if err := json.Unmarshal(r, &item); err != nil {
			// One malformed hit must not sink the rest of the response.
			continue
		}

		c := Candidate{
			Source: "zbmath",
			Title:  item.Title.Title,
			Year:   item.Year,
		}
		for _, a := range item.Contributors.Authors {
			c.Authors = append(c.Authors, a.Name)
		}
		if len(item.Source.Series) > 0 {
			c.Venue = item.Source.Series[0].Title
		}
		for _, l := range item.Links {
			if l.Type == "doi" {
				c.DOI = l.Identifier
				break
			}
		}
		hits = append(hits, c)
	}
	return hits, nil
}
