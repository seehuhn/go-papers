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

// maxBodySize caps the number of bytes read from an HTTP response body, to
// protect against unexpectedly large or malicious responses.
const maxBodySize = 10 << 20 // 10 MB

// httpOptions carries per-request settings for getJSON.
type httpOptions struct {
	Email string // for the polite pool; may be empty
}

// UserAgent returns the User-Agent string to send with outgoing requests,
// including a mailto contact when email is set. This is exported because
// internal/resolve reuses it for downloads.
func UserAgent(email string) string {
	if email == "" {
		return "paper/0.1 (+https://github.com/seehuhn/go-papers)"
	}
	return fmt.Sprintf("paper/0.1 (+https://github.com/seehuhn/go-papers; mailto:%s)", email)
}

// getJSON fetches url and decodes the JSON response body into out. Unknown
// JSON members are tolerated. Non-200 statuses become errors: a 404
// produces an error containing "not found", other statuses produce an
// error naming the status code and up to 200 bytes of the response body.
func getJSON(client *http.Client, url string, opt httpOptions, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent(opt.Email))
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("not found: %s", url)
		}
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return fmt.Errorf("unexpected status %s for %s: %s", resp.Status, url, snippet)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response from %s: %w", url, err)
	}
	return nil
}
