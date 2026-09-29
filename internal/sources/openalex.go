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
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// openAlexSelect lists the top-level work fields the client asks for.
const openAlexSelect = "id,doi,display_name,publication_year,type,cited_by_count," +
	"referenced_works,abstract_inverted_index,authorships,primary_location,locations"

// openAlexKeyHint is appended to errors that a key can cure.
const openAlexKeyHint = "; get a free key at https://openalex.org/rest-api " +
	"and store it with `paper init -openalex-key KEY <store dir>`"

// openAlexBadKeyHint replaces openAlexKeyHint when the request carried a key.
const openAlexBadKeyHint = "; the API key was rejected, replace it with " +
	"`paper init -openalex-key KEY <store dir>`"

// openAlexBatch is the most IDs one Works request carries.
const openAlexBatch = 50

// openAlexMaxPage is the largest per_page OpenAlex accepts.
const openAlexMaxPage = 200

var openAlexIDPattern = regexp.MustCompile(`^W\d+$`)

// OpenAlexWork is the part of an OpenAlex work record that paper uses.
type OpenAlexWork struct {
	ID           string // "W2100837269", the part after https://openalex.org/
	DOI          string // without the https://doi.org/ prefix, as OpenAlex gives it; "" if none
	ArxivID      string // from an arXiv DOI or an arxiv.org/abs/ landing page; "" if none
	Title        string
	Authors      []string // display names, in order
	Year         int
	Venue        string // primary_location.source.display_name; "" if none
	Type         string // OpenAlex type: "article", "preprint", "book-chapter", ...
	CitedByCount int
	References   []string // OpenAlex IDs of referenced_works, "W..."
	Abstract     string   // rebuilt from abstract_inverted_index; "" if absent
}

// openAlexRaw mirrors the JSON of one work. Unknown members are ignored.
type openAlexRaw struct {
	ID                  string           `json:"id"`
	DOI                 string           `json:"doi"`
	DisplayName         string           `json:"display_name"`
	PublicationYear     int              `json:"publication_year"`
	Type                string           `json:"type"`
	CitedByCount        int              `json:"cited_by_count"`
	ReferencedWorks     []string         `json:"referenced_works"`
	AbstractInvertedIdx map[string][]int `json:"abstract_inverted_index"`
	Authorships         []struct {
		Author struct {
			DisplayName string `json:"display_name"`
		} `json:"author"`
	} `json:"authorships"`
	PrimaryLocation *struct {
		Source *struct {
			DisplayName string `json:"display_name"`
		} `json:"source"`
	} `json:"primary_location"`
	Locations []struct {
		LandingPageURL string `json:"landing_page_url"`
	} `json:"locations"`
}

type openAlexList struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Results []openAlexRaw `json:"results"`
}

func (r *openAlexRaw) work() OpenAlexWork {
	w := OpenAlexWork{
		ID:           openAlexShortID(r.ID),
		DOI:          trimDOIPrefix(r.DOI),
		Title:        r.DisplayName,
		Year:         r.PublicationYear,
		Type:         r.Type,
		CitedByCount: r.CitedByCount,
		Abstract:     RebuildAbstract(r.AbstractInvertedIdx),
	}
	for _, a := range r.Authorships {
		w.Authors = append(w.Authors, a.Author.DisplayName)
	}
	if r.PrimaryLocation != nil && r.PrimaryLocation.Source != nil {
		w.Venue = r.PrimaryLocation.Source.DisplayName
	}
	for _, ref := range r.ReferencedWorks {
		w.References = append(w.References, openAlexShortID(ref))
	}

	// The DOI wins over a landing page.
	w.ArxivID = ArxivIDFromDOI(w.DOI)
	for _, loc := range r.Locations {
		if w.ArxivID != "" {
			break
		}
		if !strings.HasPrefix(loc.LandingPageURL, "https://arxiv.org/abs/") &&
			!strings.HasPrefix(loc.LandingPageURL, "http://arxiv.org/abs/") {
			continue
		}
		if ref := ParseRef(loc.LandingPageURL); ref.Kind == RefArxiv {
			w.ArxivID = ref.ArxivID // ParseRef drops a vN suffix
		}
	}
	return w
}

func openAlexShortID(id string) string {
	id = strings.TrimPrefix(id, "https://openalex.org/")
	return strings.TrimPrefix(id, "http://openalex.org/")
}

func trimDOIPrefix(doi string) string {
	for _, p := range []string{
		"https://doi.org/", "http://doi.org/",
		"https://dx.doi.org/", "http://dx.doi.org/",
		"doi:",
	} {
		if s, ok := strings.CutPrefix(doi, p); ok {
			return s
		}
	}
	return doi
}

// RebuildAbstract turns an OpenAlex abstract_inverted_index (word to
// positions) back into text. It returns "" for an empty index.
func RebuildAbstract(index map[string][]int) string {
	type placed struct {
		pos  int
		word string
	}
	var words []placed
	for word, positions := range index {
		for _, p := range positions {
			words = append(words, placed{p, word})
		}
	}
	sort.Slice(words, func(i, j int) bool { return words[i].pos < words[j].pos })
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = w.word
	}
	return strings.Join(parts, " ")
}

const arxivDOIPrefix = "10.48550/arxiv."

// ArxivIDFromDOI returns the arXiv ID of an arXiv DOI such as
// 10.48550/arXiv.2412.05039, and "" for any other DOI.
func ArxivIDFromDOI(doi string) string {
	if len(doi) <= len(arxivDOIPrefix) || !strings.EqualFold(doi[:len(arxivDOIPrefix)], arxivDOIPrefix) {
		return ""
	}
	return doi[len(arxivDOIPrefix):]
}

var arxivSubjectClass = regexp.MustCompile(`(?i)^([a-z-]+)\.[a-z]{2}/`)

// StripArxivClass removes the subject class from an old-style arXiv ID:
// "math.PR/0611001" becomes "math/0611001".  Other IDs are unchanged.  The
// canonical ID is the one arXiv's DataCite DOIs use.
func StripArxivClass(id string) string {
	return arxivSubjectClass.ReplaceAllString(id, "$1/")
}

// ArxivDOI returns the DataCite DOI of an arXiv ID, which is built from the
// canonical ID without subject class.
func ArxivDOI(id string) string {
	return "10.48550/arXiv." + StripArxivClass(id)
}

// OpenAlex is a client for the OpenAlex API (https://api.openalex.org).
type OpenAlex struct {
	BaseURL string // default "https://api.openalex.org"
	Client  *http.Client
	Key     string              // api_key parameter; may be empty
	Email   string              // User-Agent only
	Sleep   func(time.Duration) // nil means time.Sleep; tests stub it

	mu   sync.Mutex
	cost openAlexCost // from the responses so far
}

// openAlexCost holds what the rate limit headers of the responses so far
// said: the credits summed over all of them, and the latest remaining and
// limit values.
type openAlexCost struct {
	credits, remaining, limit float64
	hasCredits, hasRemaining  bool
	hasLimit                  bool
}

// headerFloat reads a numeric header; a missing or malformed one gives false.
func headerFloat(h http.Header, name string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(h.Get(name)), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// record adds the cost headers of a response to the totals. Credits are
// summed; remaining and limit are replaced when the response carries them.
func (o *OpenAlex) record(h http.Header) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if v, ok := headerFloat(h, "X-RateLimit-Credits-Used"); ok {
		o.cost.credits += v
		o.cost.hasCredits = true
	}
	if v, ok := headerFloat(h, "X-RateLimit-Remaining"); ok {
		o.cost.remaining, o.cost.hasRemaining = v, true
	}
	if v, ok := headerFloat(h, "X-RateLimit-Limit"); ok {
		o.cost.limit, o.cost.hasLimit = v, true
	}
}

// Spent returns the credits (dollars) the requests of this client have used
// so far, summed from X-RateLimit-Credits-Used over every response. ok is
// false until a response has carried the header; a malformed header counts
// as absent.
func (o *OpenAlex) Spent() (credits float64, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Rounding hides the noise of summing binary fractions.
	return math.Round(o.cost.credits*1e12) / 1e12, o.cost.hasCredits
}

// Remaining returns the budget left for the day as of the latest response
// that carried X-RateLimit-Remaining. ok is false if none did.
func (o *OpenAlex) Remaining() (remaining float64, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cost.remaining, o.cost.hasRemaining
}

// LastCost returns Spent and Remaining together; ok is true only if both are
// known.
func (o *OpenAlex) LastCost() (credits, remaining float64, ok bool) {
	credits, ok1 := o.Spent()
	remaining, ok2 := o.Remaining()
	return credits, remaining, ok1 && ok2
}

// LastLimit returns the day's budget from the latest response that gave
// X-RateLimit-Limit, or 0 if none did.
func (o *OpenAlex) LastLimit() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.cost.hasLimit {
		return 0
	}
	return o.cost.limit
}

// budgetExhausted reports whether se is a 429 that says the day's budget is
// used up, as opposed to a momentary rate limit.
func budgetExhausted(se *StatusError) bool {
	return se.Code == http.StatusTooManyRequests &&
		strings.Contains(strings.ToLower(se.Snippet), "budget")
}

// budgetError is the error for an exhausted day's budget. Its text names no
// key and no URL; Unwrap gives the *StatusError.
type budgetError struct{ se *StatusError }

func (e *budgetError) Error() string {
	return "openalex: HTTP 429: the day's budget is used up and further requests fail until " +
		"the budget resets at midnight UTC."
}

func (e *budgetError) Unwrap() error { return e.se }

func (o *OpenAlex) baseURL() string {
	if o.BaseURL == "" {
		return "https://api.openalex.org"
	}
	return o.BaseURL
}

// get fetches path?q and decodes the JSON reply into out. It adds select and
// api_key, waits and retries once on HTTP 429, and turns a second 429 or a
// 401 into an error that names the key setting.  A 429 that says the day's
// budget is used up is not retried and gives a budgetError.  Every response
// updates LastCost.  The URLs in errors show
// api_key=REDACTED, so that the key does not reach logs.
func (o *OpenAlex) get(path string, q url.Values, out any) error {
	q.Set("select", openAlexSelect)
	base := o.baseURL() + path + "?"
	u, shown := base+q.Encode(), base+q.Encode()
	if o.Key != "" {
		q.Set("api_key", o.Key)
		u = base + q.Encode()
		q.Set("api_key", "REDACTED")
		shown = base + q.Encode()
	}
	opt := httpOptions{Email: o.Email, ShownURL: shown, OnResponse: o.record}

	err := getJSON(o.Client, u, opt, out)
	var se *StatusError
	if errors.As(err, &se) && budgetExhausted(se) {
		return &budgetError{se}
	}
	if errors.As(err, &se) && se.Code == http.StatusTooManyRequests {
		wait := se.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		wait = min(wait, 60*time.Second)
		if o.Sleep != nil {
			o.Sleep(wait)
		} else {
			time.Sleep(wait)
		}
		err = getJSON(o.Client, u, opt, out)
		if errors.As(err, &se) && budgetExhausted(se) {
			return &budgetError{se}
		}
	}
	if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusTooManyRequests) {
		hint := openAlexKeyHint
		if se.Code == http.StatusUnauthorized && o.Key != "" {
			hint = openAlexBadKeyHint
		}
		return fmt.Errorf("openalex: HTTP %d%s: %w", se.Code, hint, err)
	}
	if err != nil {
		return fmt.Errorf("openalex: %w", err)
	}
	return nil
}

// Work fetches one work by OpenAlex ID (W...), DOI or arXiv ID.
func (o *OpenAlex) Work(id string) (*OpenAlexWork, error) {
	id = strings.TrimSpace(id)
	var path string
	if short := openAlexShortID(id); openAlexIDPattern.MatchString(short) {
		path = "/works/" + short
	} else {
		ref := ParseRef(id)
		switch ref.Kind {
		case RefDOI:
			path = "/works/doi:" + escapeDOIPath(ref.DOI)
		case RefArxiv:
			path = "/works/doi:" + escapeDOIPath(ArxivDOI(ref.ArxivID))
		default:
			return nil, fmt.Errorf("openalex: %q is not an OpenAlex ID, DOI or arXiv ID", id)
		}
	}

	var raw openAlexRaw
	if err := o.get(path, url.Values{}, &raw); err != nil {
		return nil, err
	}
	w := raw.work()
	return &w, nil
}

// escapeDOIPath escapes a DOI for use in a URL path, keeping the slashes.
func escapeDOIPath(doi string) string {
	parts := strings.Split(doi, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// Works fetches several works by OpenAlex ID, in batches of 50. The result
// follows the order of ids; IDs OpenAlex does not return are skipped.
func (o *OpenAlex) Works(ids []string) ([]OpenAlexWork, error) {
	found := make(map[string]OpenAlexWork, len(ids))
	for batch := range slices.Chunk(ids, openAlexBatch) {
		short := make([]string, len(batch))
		for i, id := range batch {
			short[i] = openAlexShortID(id)
		}
		q := url.Values{}
		q.Set("filter", "openalex_id:"+strings.Join(short, "|"))
		q.Set("per_page", strconv.Itoa(openAlexBatch))
		var list openAlexList
		if err := o.get("/works", q, &list); err != nil {
			return nil, err
		}
		for i := range list.Results {
			w := list.Results[i].work()
			found[w.ID] = w
		}
	}

	out := make([]OpenAlexWork, 0, len(ids))
	for _, id := range ids {
		if w, ok := found[openAlexShortID(id)]; ok {
			out = append(out, w)
		}
	}
	return out, nil
}

// WorksByDOI fetches the works with the given DOIs, openAlexBatch per
// request. Each DOI may be bare, "doi:"-prefixed, or a doi.org or dx.doi.org
// URL; case does not matter. The works found are returned in unspecified
// order, and duplicates are collapsed. A DOI OpenAlex does not know is
// silently absent, and an empty one is skipped.
func (o *OpenAlex) WorksByDOI(dois []string) ([]OpenAlexWork, error) {
	found := make(map[string]OpenAlexWork, len(dois))
	urls := make([]string, 0, len(dois))
	for _, d := range dois {
		d = trimDOIPrefix(strings.ToLower(strings.TrimSpace(d)))
		if d != "" {
			urls = append(urls, "https://doi.org/"+d)
		}
	}
	for batch := range slices.Chunk(urls, openAlexBatch) {
		q := url.Values{}
		q.Set("filter", "doi:"+strings.Join(batch, "|"))
		q.Set("per_page", strconv.Itoa(openAlexBatch))
		var list openAlexList
		if err := o.get("/works", q, &list); err != nil {
			return nil, err
		}
		for i := range list.Results {
			w := list.Results[i].work()
			found[w.ID] = w
		}
	}

	out := make([]OpenAlexWork, 0, len(found))
	for _, w := range found {
		out = append(out, w)
	}
	return out, nil
}

// Search runs a full-text query, optionally restricted to works published
// in year since or later. It returns up to n works (at most 200) and the
// total number of matches.
func (o *OpenAlex) Search(query string, n, since int) ([]OpenAlexWork, int, error) {
	q := url.Values{}
	q.Set("search", query)
	if since > 0 {
		q.Set("filter", sinceFilter(since))
	}
	return o.list(q, n)
}

// Citing returns works that cite workID, newest first, optionally restricted
// to works published in year since or later. It returns up to n works (at
// most 200) and the total number of citing works.
func (o *OpenAlex) Citing(workID string, n, since int) ([]OpenAlexWork, int, error) {
	filter := "cites:" + openAlexShortID(workID)
	if since > 0 {
		filter += "," + sinceFilter(since)
	}
	q := url.Values{}
	q.Set("filter", filter)
	q.Set("sort", "publication_date:desc")
	return o.list(q, n)
}

func sinceFilter(since int) string {
	return fmt.Sprintf("from_publication_date:%d-01-01", since)
}

func (o *OpenAlex) list(q url.Values, n int) ([]OpenAlexWork, int, error) {
	q.Set("per_page", strconv.Itoa(max(1, min(n, openAlexMaxPage))))
	var list openAlexList
	if err := o.get("/works", q, &list); err != nil {
		return nil, 0, err
	}
	works := make([]OpenAlexWork, len(list.Results))
	for i := range list.Results {
		works[i] = list.Results[i].work()
	}
	return works, list.Meta.Count, nil
}

// ByTitle returns the best three works whose title matches title.
func (o *OpenAlex) ByTitle(title string) ([]OpenAlexWork, error) {
	// Commas and pipes separate filters and values in the filter syntax;
	// ? and * are wildcards, which title.search rejects with HTTP 400.
	title = strings.NewReplacer(",", " ", "|", " ", "?", " ", "*", " ").Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	q := url.Values{}
	q.Set("filter", "title.search:"+title)
	q.Set("per_page", "3")
	var list openAlexList
	if err := o.get("/works", q, &list); err != nil {
		return nil, err
	}
	works := make([]OpenAlexWork, len(list.Results))
	for i := range list.Results {
		works[i] = list.Results[i].work()
	}
	return works, nil
}
