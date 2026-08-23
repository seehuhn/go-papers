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
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Handle is a client for the DOI handle system's resolver API
// (https://doi.org/api/handles/), used to confirm that a DOI exists
// regardless of which registration agency (Crossref, DataCite, ...) it
// was registered with. Crossref's works endpoint only knows about
// Crossref-registered DOIs; a DataCite-registered DOI (Zenodo, figshare,
// many arXiv-issued DOIs) 404s there even though it resolves fine. The
// handle system is the one place existence can be checked independent of
// the registrar.
type Handle struct {
	BaseURL string // default "https://doi.org"
	Client  *http.Client
}

// baseURL returns h.BaseURL, defaulting to the public handle resolver.
func (h *Handle) baseURL() string {
	if h.BaseURL == "" {
		return "https://doi.org"
	}
	return h.BaseURL
}

// handleResponse is the shape of the handle API's JSON body, as far as
// Exists needs to read it. responseCode 1 means the handle exists;
// responseCode 100 means it is unknown.
type handleResponse struct {
	ResponseCode int `json:"responseCode"`
}

// Exists reports whether doi is a registered handle, checking existence
// only — not metadata. A 200 response with responseCode 1 means the
// handle exists. A 404, or a 200 with responseCode 100, is a definitive
// "no": Exists returns (false, nil), because absence is an answer here,
// not a failure. Anything else — a 5xx, a network failure, a response
// that does not parse — leaves existence unresolved, so Exists returns
// (false, err) so callers can tell "does not exist" apart from "could
// not check".
func (h *Handle) Exists(doi string) (bool, error) {
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	u := h.baseURL() + "/api/handles/" + doi
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", UserAgent(""))
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return false, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return false, fmt.Errorf("handle: unexpected status %s for %s: %s", resp.Status, u, snippet)
	}

	var parsed handleResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, fmt.Errorf("handle: decoding response from %s: %w", u, err)
	}
	switch parsed.ResponseCode {
	case 1:
		return true, nil
	case 100:
		return false, nil
	default:
		return false, fmt.Errorf("handle: unexpected responseCode %d for %s", parsed.ResponseCode, u)
	}
}
