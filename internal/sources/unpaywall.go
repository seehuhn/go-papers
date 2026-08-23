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
)

type OALocation struct {
	PDFURL   string `json:"url_for_pdf"`
	License  string `json:"license"`
	HostType string `json:"host_type"` // "publisher" | "repository"
}

type UnpaywallResult struct {
	IsOA           bool        `json:"is_oa"`
	BestOALocation *OALocation `json:"best_oa_location"`
}

type Unpaywall struct {
	BaseURL string // default "https://api.unpaywall.org"
	Client  *http.Client
	Email   string // REQUIRED by the API
}

// baseURL returns u.BaseURL, defaulting to the public Unpaywall API.
func (u *Unpaywall) baseURL() string {
	if u.BaseURL == "" {
		return "https://api.unpaywall.org"
	}
	return u.BaseURL
}

// Lookup queries the OA status of a DOI. Calling with an empty Email
// returns an error saying how to configure one.
func (u *Unpaywall) Lookup(doi string) (*UnpaywallResult, error) {
	if u.Email == "" {
		return nil, fmt.Errorf("unpaywall requires a contact email: run `paper init -email <address> <dir>` to set one")
	}

	q := url.Values{}
	q.Set("email", u.Email)
	urlStr := u.baseURL() + "/v2/" + doi + "?" + q.Encode()

	var result UnpaywallResult
	if err := getJSON(u.Client, urlStr, httpOptions{Email: u.Email}, &result); err != nil {
		return nil, fmt.Errorf("unpaywall: fetching %s: %w", doi, err)
	}
	return &result, nil
}
