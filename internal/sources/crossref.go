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
	"strconv"
)

// CrossrefAuthor is one author entry in a Crossref work record. Most
// entries carry Family/Given; corporate or collective authors (e.g. "LIGO
// Scientific Collaboration") instead carry a literal Name with Family and
// Given both absent.
type CrossrefAuthor struct {
	Family string `json:"family"`
	Given  string `json:"given"`
	Name   string `json:"name"`
}

// CrossrefDate is a Crossref date field, given as a list of date parts
// (year, month, day), with trailing parts omitted when unknown.
type CrossrefDate struct {
	DateParts [][]int `json:"date-parts"`
}

// Year returns the publication year, or 0 when it is absent.
func (d CrossrefDate) Year() int {
	if len(d.DateParts) == 0 || len(d.DateParts[0]) == 0 {
		return 0
	}
	return d.DateParts[0][0]
}

// CrossrefWork is the metadata for one work, as returned by the Crossref
// API.
type CrossrefWork struct {
	DOI            string           `json:"DOI"`
	Type           string           `json:"type"` // "journal-article", "book", ...
	Titles         []string         `json:"title"`
	ContainerTitle []string         `json:"container-title"`
	Authors        []CrossrefAuthor `json:"author"`
	Volume         string           `json:"volume"`
	Issue          string           `json:"issue"`
	Page           string           `json:"page"`
	ISSN           []string         `json:"ISSN"`
	Publisher      string           `json:"publisher"`
	Score          float64          `json:"score"` // only on search results
	Published      CrossrefDate     `json:"published"`
	Abstract       string           `json:"abstract"`
}

// crossrefWorkMessage is the envelope Crossref uses for a single-work
// response.
type crossrefWorkMessage struct {
	Message CrossrefWork `json:"message"`
}

// crossrefSearchMessage is the envelope Crossref uses for a search
// response.
type crossrefSearchMessage struct {
	Message struct {
		Items []*CrossrefWork `json:"items"`
	} `json:"message"`
}

// Crossref is a client for the Crossref REST API
// (https://api.crossref.org).
type Crossref struct {
	BaseURL string // default "https://api.crossref.org"
	Client  *http.Client
	Email   string
}

// baseURL returns c.BaseURL, defaulting to the public Crossref API.
func (c *Crossref) baseURL() string {
	if c.BaseURL == "" {
		return "https://api.crossref.org"
	}
	return c.BaseURL
}

// Work fetches metadata for one DOI. A 404 returns an error wrapping
// ErrNotFound.
func (c *Crossref) Work(doi string) (*CrossrefWork, error) {
	u := c.baseURL() + "/works/" + doi
	if c.Email != "" {
		u += "?" + url.Values{"mailto": {c.Email}}.Encode()
	}

	var msg crossrefWorkMessage
	if err := getJSON(c.Client, u, httpOptions{Email: c.Email}, &msg); err != nil {
		return nil, fmt.Errorf("crossref: fetching work %s: %w", doi, err)
	}
	return &msg.Message, nil
}

// Search runs a bibliographic query and returns up to rows results in
// relevance order, with Score populated.
func (c *Crossref) Search(query string, rows int) ([]*CrossrefWork, error) {
	q := url.Values{}
	q.Set("query.bibliographic", query)
	q.Set("rows", strconv.Itoa(rows))
	if c.Email != "" {
		q.Set("mailto", c.Email)
	}
	u := c.baseURL() + "/works?" + q.Encode()

	var msg crossrefSearchMessage
	if err := getJSON(c.Client, u, httpOptions{Email: c.Email}, &msg); err != nil {
		return nil, fmt.Errorf("crossref: searching %q: %w", query, err)
	}
	return msg.Message.Items, nil
}
