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
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// maxBodySize caps the number of bytes read from an HTTP response body, to
// protect against unexpectedly large or malicious responses.
const maxBodySize = 10 << 20 // 10 MB

// ErrNotFound is the sentinel a client wraps into its returned error when
// the remote service reports that the requested resource does not exist
// (a Crossref/arXiv 404, or an arXiv query whose feed comes back empty).
// A caller that needs to tell "this identifier does not exist" apart from
// every other kind of failure (a 5xx, a timeout, a malformed response)
// must check with errors.Is(err, sources.ErrNotFound) rather than
// matching on the error text: the non-404 branch of getJSON below embeds
// up to 200 raw bytes of the response body, which could itself contain
// the words "not found" (a rate-limit page, an HTML error page, ...), so
// string-matching on that text is not safe.
var ErrNotFound = errors.New("not found")

// StatusError is the error getJSON returns for an HTTP status other than 200
// and 404. Callers that need the code (a rate limit, a rejected key) find it
// with errors.As.
type StatusError struct {
	Code       int
	RetryAfter time.Duration // from the Retry-After header, 0 if absent
	URL        string
	Snippet    string // first 200 bytes of the body

	status string // the full status line, e.g. "429 Too Many Requests"
}

func (e *StatusError) Error() string {
	status := e.status
	if status == "" {
		status = fmt.Sprintf("%d %s", e.Code, http.StatusText(e.Code))
	}
	return fmt.Sprintf("unexpected status %s for %s: %s", status, e.URL, e.Snippet)
}

// parseRetryAfter reads a Retry-After header, either a number of seconds or
// an HTTP date. It returns 0 if the header is absent or unreadable.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// httpOptions carries per-request settings for getJSON.
type httpOptions struct {
	Email string // for the polite pool; may be empty

	// ShownURL, if set, replaces the request URL in every error getJSON
	// returns. A client whose URLs carry a secret sets it to a copy of the
	// URL with the secret removed.
	ShownURL string
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
// produces an error wrapping ErrNotFound, other statuses produce a *StatusError
// naming the status code and up to 200 bytes of the response body.
func getJSON(client *http.Client, url string, opt httpOptions, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	shown := url
	if opt.ShownURL != "" {
		shown = opt.ShownURL
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return showURLError(err, shown)
	}
	req.Header.Set("User-Agent", UserAgent(opt.Email))
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return showURLError(err, shown)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrNotFound, shown)
		}
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return &StatusError{
			Code:       resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			URL:        shown,
			Snippet:    string(snippet),
			status:     resp.Status,
		}
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response from %s: %w", shown, err)
	}
	return nil
}

// showURLError replaces the URL in a *url.Error by shown, so that a
// transport error does not reveal secrets in the request URL.
func showURLError(err error, shown string) error {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		cp := *ue
		cp.URL = shown
		return &cp
	}
	return err
}
