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
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

const openAlexHoeffding = `{
  "id": "https://openalex.org/W2100837269",
  "doi": "https://doi.org/10.1080/01621459.1963.10500830",
  "display_name": "Probability Inequalities for Sums of Bounded Random Variables",
  "publication_year": 1963,
  "type": "article",
  "cited_by_count": 9000,
  "referenced_works": [
    "https://openalex.org/W1", "https://openalex.org/W2", "https://openalex.org/W3",
    "https://openalex.org/W4", "https://openalex.org/W5", "https://openalex.org/W6",
    "https://openalex.org/W7", "https://openalex.org/W8", "https://openalex.org/W9",
    "https://openalex.org/W10"
  ],
  "abstract_inverted_index": {"Upper":[0],"bounds":[1],"are":[2],"derived.":[3]},
  "authorships": [
    {"author": {"display_name": "Wassily Hoeffding"}},
    {"author": {"display_name": "Second Author"}}
  ],
  "primary_location": {"source": {"display_name": "Journal of the American Statistical Association"},
                       "landing_page_url": "https://doi.org/10.1080/01621459.1963.10500830"},
  "locations": [{"landing_page_url": "https://doi.org/10.1080/01621459.1963.10500830"}]
}`

// openAlexList wraps works in a list response with the given meta.count.
func oaListJSON(count int, works ...string) string {
	return fmt.Sprintf(`{"meta":{"count":%d},"results":[%s]}`, count, strings.Join(works, ","))
}

func openAlexServer(t *testing.T, h http.HandlerFunc) *OpenAlex {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &OpenAlex{BaseURL: srv.URL, Client: srv.Client(), Key: "k",
		Sleep: func(time.Duration) {}}
}

func TestRebuildAbstract(t *testing.T) {
	idx := map[string][]int{
		"We": {0}, "prove": {1}, "that": {2, 8}, "the": {3, 9},
		"estimator": {4}, "is": {5, 11}, "consistent": {6}, "and": {7},
		"rate": {10}, "optimal.": {12},
	}
	want := "We prove that the estimator is consistent and that the rate is optimal."
	if got := RebuildAbstract(idx); got != want {
		t.Errorf("RebuildAbstract = %q, want %q", got, want)
	}
	if got := RebuildAbstract(nil); got != "" {
		t.Errorf("RebuildAbstract(nil) = %q, want empty", got)
	}
}

func TestArxivIDFromDOI(t *testing.T) {
	for _, c := range []struct{ doi, want string }{
		{"10.48550/arXiv.2412.05039", "2412.05039"},
		{"10.48550/ARXIV.math.PR/0611001", "math.PR/0611001"},
		{"10.1080/x", ""},
	} {
		if got := ArxivIDFromDOI(c.doi); got != c.want {
			t.Errorf("ArxivIDFromDOI(%q) = %q, want %q", c.doi, got, c.want)
		}
	}
}

func TestOpenAlexWorkByDOI(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/doi:10.1080/01621459.1963.10500830" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("api_key") != "k" {
			t.Errorf("api_key = %q", q.Get("api_key"))
		}
		if !strings.Contains(q.Get("select"), "abstract_inverted_index") {
			t.Errorf("select = %q", q.Get("select"))
		}
		io.WriteString(w, openAlexHoeffding)
	})
	w, err := o.Work("https://doi.org/10.1080/01621459.1963.10500830")
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "W2100837269" || w.DOI != "10.1080/01621459.1963.10500830" ||
		w.Year != 1963 || w.Type != "article" || w.CitedByCount != 9000 ||
		w.Title != "Probability Inequalities for Sums of Bounded Random Variables" ||
		w.Venue != "Journal of the American Statistical Association" ||
		w.Abstract != "Upper bounds are derived." || w.ArxivID != "" {
		t.Errorf("work = %+v", w)
	}
	if len(w.References) != 10 || w.References[0] != "W1" || w.References[9] != "W10" {
		t.Errorf("references = %v", w.References)
	}
	if !slices.Equal(w.Authors, []string{"Wassily Hoeffding", "Second Author"}) {
		t.Errorf("authors = %v", w.Authors)
	}
}

func TestOpenAlexWorkByOpenAlexID(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/W2100837269" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, openAlexHoeffding)
	})
	if _, err := o.Work("W2100837269"); err != nil {
		t.Fatal(err)
	}
}

const openAlexArxivWork = `{
  "id": "https://openalex.org/W42",
  "doi": "https://doi.org/10.48550/arxiv.math.PR/0611001",
  "display_name": "An old-style preprint",
  "publication_year": 2006,
  "type": "preprint",
  "cited_by_count": 1,
  "referenced_works": [],
  "abstract_inverted_index": null,
  "authorships": [],
  "primary_location": null,
  "locations": [{"landing_page_url": "https://arxiv.org/abs/math/0611001v2"}]
}`

func TestOpenAlexWorkByArxiv(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/doi:10.48550/arXiv.math.PR/0611001" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, openAlexArxivWork)
	})
	w, err := o.Work("math.PR/0611001")
	if err != nil {
		t.Fatal(err)
	}
	// The DOI wins over the landing page.
	if w.ArxivID != "math.PR/0611001" {
		t.Errorf("ArxivID = %q", w.ArxivID)
	}
	if w.Venue != "" {
		t.Errorf("Venue = %q, want empty for a null primary_location", w.Venue)
	}
}

func TestOpenAlexArxivFromLandingPage(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Replace(openAlexArxivWork,
			`"doi": "https://doi.org/10.48550/arxiv.math.PR/0611001"`, `"doi": null`, 1))
	})
	w, err := o.Work("W42")
	if err != nil {
		t.Fatal(err)
	}
	if w.DOI != "" || w.ArxivID != "math/0611001" {
		t.Errorf("DOI = %q, ArxivID = %q", w.DOI, w.ArxivID)
	}
}

func TestOpenAlexWorkNotFound(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	_, err := o.Work("W1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestOpenAlexWorksBatches(t *testing.T) {
	var ids []string
	for i := 1; i <= 120; i++ {
		ids = append(ids, fmt.Sprintf("W%d", i))
	}
	var sizes []int
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("per_page") != "50" {
			t.Errorf("per_page = %q", q.Get("per_page"))
		}
		f, ok := strings.CutPrefix(q.Get("filter"), "openalex_id:")
		if !ok {
			t.Errorf("filter = %q", q.Get("filter"))
		}
		req := strings.Split(f, "|")
		sizes = append(sizes, len(req))
		// answer in reverse order, and leave out W7
		var works []string
		for i := len(req) - 1; i >= 0; i-- {
			if req[i] == "W7" {
				continue
			}
			works = append(works, fmt.Sprintf(`{"id":"https://openalex.org/%s"}`, req[i]))
		}
		io.WriteString(w, oaListJSON(len(works), works...))
	})
	got, err := o.Works(ids)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sizes, []int{50, 50, 20}) {
		t.Errorf("batch sizes = %v", sizes)
	}
	var want []string
	for _, id := range ids {
		if id != "W7" {
			want = append(want, id)
		}
	}
	var have []string
	for _, w := range got {
		have = append(have, w.ID)
	}
	if !slices.Equal(have, want) {
		t.Errorf("IDs = %v", have)
	}
}

func TestOpenAlexSearchQuery(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/works" || q.Get("search") != "hoeffding bounds" ||
			q.Get("per_page") != "200" || q.Get("filter") != "from_publication_date:2015-01-01" ||
			q.Get("api_key") != "k" {
			t.Errorf("url = %s", r.URL)
		}
		io.WriteString(w, oaListJSON(1234, openAlexHoeffding))
	})
	got, count, err := o.Search("hoeffding bounds", 500, 2015)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1234 || len(got) != 1 {
		t.Errorf("count = %d, works = %d", count, len(got))
	}
}

func TestOpenAlexSearchNoSince(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("filter") {
			t.Errorf("unexpected filter in %s", r.URL)
		}
		io.WriteString(w, oaListJSON(0))
	})
	if _, _, err := o.Search("x", 10, 0); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAlexCitingQuery(t *testing.T) {
	var works []string
	for i := 0; i < 50; i++ {
		works = append(works, fmt.Sprintf(`{"id":"https://openalex.org/W%d"}`, 100+i))
	}
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter") != "cites:W1,from_publication_date:2021-01-01" ||
			q.Get("sort") != "publication_date:desc" || q.Get("per_page") != "50" {
			t.Errorf("url = %s", r.URL)
		}
		io.WriteString(w, oaListJSON(900, works...))
	})
	got, count, err := o.Citing("W1", 50, 2021)
	if err != nil {
		t.Fatal(err)
	}
	if count != 900 || len(got) != 50 {
		t.Errorf("count = %d, works = %d", count, len(got))
	}
}

func TestOpenAlexByTitle(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter") != "title.search:Probability inequalities" || q.Get("per_page") != "3" {
			t.Errorf("url = %s", r.URL)
		}
		io.WriteString(w, oaListJSON(1, openAlexHoeffding))
	})
	got, err := o.ByTitle("Probability inequalities")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "W2100837269" {
		t.Errorf("got = %+v", got)
	}
}

func TestOpenAlexNoAbstract(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Replace(openAlexHoeffding,
			`{"Upper":[0],"bounds":[1],"are":[2],"derived.":[3]}`, `null`, 1))
	})
	w, err := o.Work("W2100837269")
	if err != nil {
		t.Fatal(err)
	}
	if w.Abstract != "" {
		t.Errorf("Abstract = %q", w.Abstract)
	}
}

func TestOpenAlexRetryOn429(t *testing.T) {
	calls := 0
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, openAlexHoeffding)
	})
	var slept []time.Duration
	o.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if _, err := o.Work("W2100837269"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(slept) != 1 || slept[0] != 7*time.Second {
		t.Errorf("calls = %d, slept = %v", calls, slept)
	}
}

func TestOpenAlexRetryAfterBounds(t *testing.T) {
	for _, c := range []struct {
		header string
		want   time.Duration
	}{
		{"", time.Second},
		{"3600", 60 * time.Second},
	} {
		calls := 0
		o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			io.WriteString(w, openAlexHoeffding)
		})
		var slept []time.Duration
		o.Sleep = func(d time.Duration) { slept = append(slept, d) }
		if _, err := o.Work("W1"); err != nil {
			t.Fatal(err)
		}
		if len(slept) != 1 || slept[0] != c.want {
			t.Errorf("Retry-After %q: slept = %v, want %v", c.header, slept, c.want)
		}
	}
}

func TestOpenAlexTwo429s(t *testing.T) {
	calls := 0
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := o.Work("W1")
	if err == nil || !strings.Contains(err.Error(), "paper init -openalex-key") {
		t.Errorf("err = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestOpenAlex401(t *testing.T) {
	o := openAlexServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := o.Work("W1")
	if err == nil || !strings.Contains(err.Error(), "paper init -openalex-key") {
		t.Errorf("err = %v", err)
	}
}

func TestStatusErrorText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	err := getJSON(srv.Client(), srv.URL+"/x", httpOptions{}, new(any))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 502 {
		t.Fatalf("err = %v", err)
	}
	want := "unexpected status 502 Bad Gateway for " + srv.URL + "/x: boom\n"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
