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

package main

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

// overrideOpenAlex points openAlexBase at url for the rest of the test.
func overrideOpenAlex(t *testing.T, url string) {
	t.Helper()
	old := openAlexBase
	t.Cleanup(func() { openAlexBase = old })
	openAlexBase = url
}

// openAlexServer serves body for every request and records the last
// query string in *query when query is not nil.
func openAlexServer(t *testing.T, body string, query *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query != nil {
			*query = r.URL.RawQuery
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHeldIndexMatchesDOICaseInsensitively(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("abc_2000")
	p.DOI = "10.1080/abc"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q := cleanPaper("prob_2024")
	q.DOI = ""
	q.Arxiv = &store.ArxivRef{ID: "2412.05039"}
	if err := s.Save(q); err != nil {
		t.Fatal(err)
	}

	h, err := loadHeld(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.key(&sources.OpenAlexWork{DOI: "10.1080/ABC"}); got != "abc_2000" {
		t.Errorf("DOI 10.1080/ABC: held key %q, want abc_2000", got)
	}
	if got := h.key(&sources.OpenAlexWork{ArxivID: "2412.05039"}); got != "prob_2024" {
		t.Errorf("arXiv 2412.05039: held key %q, want prob_2024", got)
	}
	if got := h.key(&sources.OpenAlexWork{DOI: "10.1/none", ArxivID: "9999.99999"}); got != "" {
		t.Errorf("unheld work: held key %q, want empty", got)
	}
	if got := h.key(&sources.OpenAlexWork{}); got != "" {
		t.Errorf("work without identifiers: held key %q, want empty", got)
	}
}

func TestHeldIndexOldStyleArxivIDs(t *testing.T) {
	s, _ := fixtureStore(t)
	// The store may spell the ID with or without the subject class.
	p := cleanPaper("with_class")
	p.DOI = ""
	p.Arxiv = &store.ArxivRef{ID: "math.PR/0611001"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q := cleanPaper("without_class")
	q.DOI = ""
	q.Arxiv = &store.ArxivRef{ID: "hep-th/9901001"}
	if err := s.Save(q); err != nil {
		t.Fatal(err)
	}
	h, err := loadHeld(s)
	if err != nil {
		t.Fatal(err)
	}
	// OpenAlex gives the canonical ID, without the class.
	for id, want := range map[string]string{
		"math/0611001":    "with_class",
		"math.PR/0611001": "with_class",
		"hep-th/9901001":  "without_class",
	} {
		if got := h.key(&sources.OpenAlexWork{ArxivID: id}); got != want {
			t.Errorf("arXiv %s: held key %q, want %q", id, got, want)
		}
	}
}

func sampleLines() []workLine {
	return []workLine{
		{
			Work: sources.OpenAlexWork{
				ID: "W2100837269", DOI: "10.1080/01621459.1963.10500830",
				Title:   "Probability Inequalities for Sums of Bounded Random Variables",
				Authors: []string{"Wassily Hoeffding"}, Year: 1963,
				Venue:        "Journal of the American Statistical Association",
				CitedByCount: 4870,
			},
			Held: "hoeffding_1963",
		},
		{
			Work: sources.OpenAlexWork{
				ID: "W42", ArxivID: "2412.05039", Title: "A new bound",
				Authors: []string{"Ada Lovelace", "Alan Turing"}, Year: 2024,
				Venue: "arXiv", Type: "preprint", CitedByCount: 3,
				Abstract: "We prove a bound.",
			},
			CitedByYours: 2,
			CitesYours:   1,
		},
	}
}

func TestPrintWorksText(t *testing.T) {
	var buf bytes.Buffer
	if err := printWorks(&buf, 57, sampleLines(), false, false); err != nil {
		t.Fatal(err)
	}
	want := "total: 57, shown: 2\n" +
		"W2100837269 | 1963 | Hoeffding | Probability Inequalities for Sums of Bounded Random Variables | Journal of the American Statistical Association | cited 4870 | doi:10.1080/01621459.1963.10500830 | held:hoeffding_1963\n" +
		"    abstract: none\n" +
		"W42 | 2024 | Lovelace | A new bound | arXiv | cited 3 | cited-by-yours 2 | cites-yours 1 | arxiv:2412.05039\n" +
		"    abstract: We prove a bound.\n"
	if got := buf.String(); got != want {
		t.Errorf("text output:\ngot:\n%s\nwant:\n%s", got, want)
	}

	buf.Reset()
	if err := printWorks(&buf, 57, sampleLines(), true, false); err != nil {
		t.Fatal(err)
	}
	want = "total: 57, shown: 2\n" +
		"W2100837269 | 1963 | Hoeffding | Probability Inequalities for Sums of Bounded Random Variables | Journal of the American Statistical Association | cited 4870 | doi:10.1080/01621459.1963.10500830 | held:hoeffding_1963\n" +
		"W42 | 2024 | Lovelace | A new bound | arXiv | cited 3 | cited-by-yours 2 | cites-yours 1 | arxiv:2412.05039\n"
	if got := buf.String(); got != want {
		t.Errorf("short text output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintWorksJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printWorks(&buf, 57, sampleLines(), false, true); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total int              `json:"total"`
		Works []map[string]any `json:"works"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.Total != 57 || len(got.Works) != 2 {
		t.Fatalf("total %d, %d works; want 57 and 2", got.Total, len(got.Works))
	}
	first, second := got.Works[0], got.Works[1]
	for _, k := range []string{"id", "doi", "year", "authors", "title", "venue", "type", "cited_by_count", "held"} {
		if _, ok := first[k]; !ok {
			t.Errorf("first work lacks key %q: %v", k, first)
		}
	}
	for _, k := range []string{"arxiv", "abstract", "cited_by_yours", "cites_yours"} {
		if _, ok := first[k]; ok {
			t.Errorf("first work has key %q, want it omitted", k)
		}
	}
	if _, ok := second["held"]; ok {
		t.Errorf("unheld work has a held key: %v", second)
	}
	if _, ok := second["doi"]; ok {
		t.Errorf("work without DOI has a doi key: %v", second)
	}
	if second["arxiv"] != "2412.05039" || second["cited_by_yours"] != 2.0 || second["cites_yours"] != 1.0 {
		t.Errorf("second work: %v", second)
	}

	buf.Reset()
	printWorks(&buf, 0, nil, false, true)
	if !bytes.Contains(buf.Bytes(), []byte(`"works": []`)) {
		t.Errorf("empty result should give an empty works array:\n%s", buf.String())
	}
}

// openAlexCostServer serves refsWork and refsList with the three rate limit
// headers set to credits, remaining and limit, and points openAlexBase at
// itself.
func openAlexCostServer(t *testing.T, credits, remaining, limit string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Credits-Used", credits)
		w.Header().Set("X-RateLimit-Remaining", remaining)
		w.Header().Set("X-RateLimit-Limit", limit)
		if r.URL.Path == "/works/W100" {
			w.Write([]byte(refsWork))
			return
		}
		w.Write([]byte(refsList))
	}))
	t.Cleanup(srv.Close)
	overrideOpenAlex(t, srv.URL)
}

func TestLogOpenAlexCarriesCost(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexCostServer(t, "10", "4200", "10000")
	var err error
	captureStdout(t, func() { err = runRefs([]string{"W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Credits   *float64 `json:"credits"`
		Remaining *float64 `json:"remaining"`
	}
	log := strings.TrimSpace(readEventLog(t, dir))
	if err := json.Unmarshal([]byte(log), &ev); err != nil {
		t.Fatalf("event %q: %v", log, err)
	}
	if ev.Credits == nil || *ev.Credits != 20 || ev.Remaining == nil || *ev.Remaining != 4200 {
		t.Errorf("event lacks credits 20 (two requests of 10) and remaining 4200: %s", log)
	}
}

func TestLogOpenAlexOmitsUnknownCost(t *testing.T) {
	_, dir := fixtureStore(t)
	openAlexRoutes(t, map[string]string{"/works/W100": refsWork, "/works": refsList})
	var err error
	captureStdout(t, func() { err = runRefs([]string{"W100"}) })
	if err != nil {
		t.Fatal(err)
	}
	log := readEventLog(t, dir)
	if strings.Contains(log, `"credits"`) || strings.Contains(log, `"remaining"`) {
		t.Errorf("event carries cost fields although no header was sent:\n%s", log)
	}
}

func TestLowBudgetWarning(t *testing.T) {
	for _, tc := range []struct {
		remaining, limit string
		want             bool
		line             string
	}{
		{"500", "10000", true, "openalex: 500 of 10000 credits left today\n"},
		{"5000", "10000", false, ""},
		{"900", "", true, "openalex: 900 of 10000 credits left today\n"}, // no limit header: 10000
		{"1500", "", false, ""},
		{"1500", "20000", true, "openalex: 1500 of 20000 credits left today\n"},
	} {
		t.Run(tc.remaining+"/"+tc.limit, func(t *testing.T) {
			fixtureStore(t)
			openAlexCostServer(t, "10", tc.remaining, tc.limit)
			var err error
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() { err = runRefs([]string{"W100"}) })
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want && !strings.Contains(stderr, tc.line) {
				t.Errorf("stderr %q lacks the warning %q", stderr, tc.line)
			}
			if !tc.want && strings.Contains(stderr, "credits left") {
				t.Errorf("stderr %q has an unwanted warning", stderr)
			}
			if strings.Count(stderr, "credits left") > 1 {
				t.Errorf("warning repeated: %q", stderr)
			}
			if strings.Contains(stdout, "credits left") {
				t.Errorf("warning on stdout: %q", stdout)
			}
		})
	}
}

func TestWarnLowBudgetWithoutResponse(t *testing.T) {
	var buf bytes.Buffer
	oa := &sources.OpenAlex{}
	warnLowBudget(&buf, oa)
	if buf.Len() != 0 {
		t.Errorf("warning without any response: %q", buf.String())
	}
}

func TestBudget429EventAndWarning(t *testing.T) {
	_, dir := fixtureStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Limit", "10000")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"Insufficient budget"}`))
	}))
	t.Cleanup(srv.Close)
	overrideOpenAlex(t, srv.URL)

	var err error
	stderr := captureStderr(t, func() {
		captureStdout(t, func() { err = runRefs([]string{"W100"}) })
	})
	if err == nil || !strings.Contains(err.Error(), "credits are used up") ||
		!strings.HasSuffix(err.Error(), "the budget resets at midnight UTC.") {
		t.Fatalf("err = %v", err)
	}
	log := readEventLog(t, dir)
	if !strings.Contains(log, `"remaining":0`) || strings.Contains(log, `"credits"`) {
		t.Errorf("event should carry remaining 0 and no credits:\n%s", log)
	}
	if !strings.Contains(stderr, "openalex: 0 of 10000 credits left today\n") {
		t.Errorf("stderr lacks the warning: %q", stderr)
	}
}
