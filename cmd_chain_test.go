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
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chainFixture serves a small OpenAlex for "paper chain": works by ID,
// batches by openalex_id filter, citations by cites filter and works by
// DOI filter. Unknown work IDs answer 404. It records the requests as
// "path?query".
//
// The graph: anchors W1 and W2; W1 cites W10 and W11; W2 cites W11, W12
// and W1; W20 cites W1; W21 cites W1 and W2; W22 cites W2; W12 cites W30
// (a work only the bibliography holds); W22 also cites W31, a work
// without DOI that the bibliography can only name by title, and W31 cites
// W20. W21 is cited more often than W11.
func chainFixture(t *testing.T) *openAlexRecorder {
	t.Helper()
	ws := map[string]relatedWork{}
	for _, w := range []relatedWork{
		{id: "W1", doi: "10.1000/a1", title: "Anchor one about the estimation of lattices", year: 2015, cited: 50,
			refs: []string{"W10", "W11"}},
		{id: "W2", doi: "10.1000/a2", title: "Anchor two on random walks in a strip", year: 2016, cited: 40,
			refs: []string{"W11", "W12", "W1"}},
		{id: "W10", doi: "10.1000/x10", title: "Perturbation bounds for tridiagonal systems", year: 1990, cited: 10},
		{id: "W11", doi: "10.1000/x11", title: "Concentration of measure on product spaces", year: 1991, cited: 11},
		{id: "W12", doi: "10.1000/x12", title: "Spectral gaps of sparse graphs", year: 1992, cited: 12,
			refs: []string{"W30"}},
		{id: "W20", doi: "10.1000/x20", title: "Heavy tails in queueing networks", year: 2019, cited: 3,
			refs: []string{"W1"}},
		{id: "W21", doi: "10.1000/x21", title: "Sampling from log-concave densities", year: 2021, cited: 30,
			refs: []string{"W1", "W2"}},
		{id: "W22", doi: "10.1000/x22", title: "Optimal transport for image registration", year: 2022, cited: 2,
			refs: []string{"W2", "W31"}},
		{id: "W31", title: "Tidal marshes and the carbon cycle", year: 2001, cited: 7,
			refs: []string{"W20"}},
		{id: "W30", doi: "10.1000/x30", title: "Martingale methods in finance", year: 1985, cited: 99},
	} {
		ws[w.id] = w
	}
	citing := map[string][]string{"W1": {"W20", "W21", "W2"}, "W2": {"W21", "W22"}}
	list := func(ids []string) string {
		var parts []string
		for _, id := range ids {
			if w, ok := ws[id]; ok {
				parts = append(parts, w.json())
			}
		}
		return fmt.Sprintf(`{"meta": {"count": %d}, "results": [%s]}`, len(parts), strings.Join(parts, ","))
	}
	rec := &openAlexRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path + "?" + r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if id, ok := strings.CutPrefix(r.URL.Path, "/works/"); ok {
			if work, ok := ws[id]; ok {
				w.Write([]byte(work.json()))
				return
			}
		}
		if r.URL.Path == "/works" {
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.HasPrefix(filter, "openalex_id:"):
				w.Write([]byte(list(strings.Split(strings.TrimPrefix(filter, "openalex_id:"), "|"))))
				return
			case strings.HasPrefix(filter, "cites:"):
				id, _, _ := strings.Cut(strings.TrimPrefix(filter, "cites:"), ",")
				w.Write([]byte(list(citing[id])))
				return
			case strings.HasPrefix(filter, "title.search:"):
				var ids []string
				for _, work := range ws {
					if work.title == strings.TrimPrefix(filter, "title.search:") {
						ids = append(ids, work.id)
					}
				}
				w.Write([]byte(list(ids)))
				return
			case strings.HasPrefix(filter, "doi:"):
				var ids []string
				for _, d := range strings.Split(strings.TrimPrefix(filter, "doi:"), "|") {
					for _, work := range ws {
						if "https://doi.org/"+work.doi == d {
							ids = append(ids, work.id)
						}
					}
				}
				w.Write([]byte(list(ids)))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	overrideOpenAlex(t, srv.URL)
	return rec
}

// chainOrder returns the IDs of the work lines below the "total:" line of
// plain chain output, in order.
func chainOrder(out string) []string {
	_, rest, _ := strings.Cut(out, "\ntotal: ")
	var order []string
	for _, l := range strings.Split(rest, "\n") {
		if id, _, ok := strings.Cut(l, " | "); ok && !strings.HasPrefix(l, " ") {
			order = append(order, id)
		}
	}
	return order
}

func TestChainMergesAndRanks(t *testing.T) {
	fixtureStore(t)
	chainFixture(t)
	var err error
	out := captureStdout(t, func() { err = runChain([]string{"-short", "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	var anchors []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "anchor: ") {
			anchors = append(anchors, l)
		}
	}
	if len(anchors) != 2 {
		t.Errorf("%d anchor lines, want 2:\n%s", len(anchors), out)
	}
	if len(anchors) == 2 && (!strings.HasPrefix(anchors[0], "anchor: W1 | ") || !strings.HasSuffix(anchors[0], " | refs 2 | citing 3 of 3") ||
		!strings.HasPrefix(anchors[1], "anchor: W2 | ") || !strings.HasSuffix(anchors[1], " | refs 3 | citing 2 of 2")) {
		t.Errorf("anchor lines wrong:\n%s", out)
	}
	if !strings.Contains(out, "total: 6, shown: 6\n") {
		t.Errorf("no total line:\n%s", out)
	}
	order := chainOrder(out)
	// W21 and W11 are linked to both anchors, W21 is cited more often; the
	// others follow by citation count.
	want := []string{"W21", "W11", "W12", "W10", "W20", "W22"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("order %v, want %v:\n%s", order, want, out)
	}
	works := worksIn(out)
	for id, k := range map[string]string{"W21": "anchors 2", "W11": "anchors 2", "W10": "anchors 1"} {
		if !strings.Contains(works[id], " | "+k) {
			t.Errorf("%s line %q lacks %q", id, works[id], k)
		}
	}
	if strings.Contains(works["W10"], "bib ") {
		t.Errorf("W10 line %q has a bib count without -bib", works["W10"])
	}
}

// writeChainBib writes a .bib file into a new directory and returns its path.
func writeChainBib(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "refs.bib")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChainBibExcludesAndCounts(t *testing.T) {
	fixtureStore(t)
	rec := chainFixture(t)
	bib := writeChainBib(t, `@article{eleven1991,
  author = {Ed Eleven},
  title = {Concentration of measure on product spaces},
  year = {1991},
  doi = {10.1000/X11},
}

@article{thirty1985,
  author = {Tom Thirty},
  title = {Martingale methods in finance},
  year = {1985},
  doi = {10.1000/x30},
}

@article{untraceable2000,
  author = {Uma Untraceable},
  title = {A paper with neither DOI nor arXiv ID},
  year = {2000},
}
`)
	var err error
	out := captureStdout(t, func() { err = runChain([]string{"-short", "-bib", bib, "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	if l, ok := works["W11"]; ok {
		t.Errorf("W11 is listed, but the bibliography cites it: %s", l)
	}
	if !strings.Contains(out, "total: 5, shown: 5\n") {
		t.Errorf("total after exclusion:\n%s", out)
	}
	if !strings.Contains(out, "\nbib: resolved 2 of 3 entries\ntotal: ") {
		t.Errorf("no bib line between the anchors and the total:\n%s", out)
	}
	if l := works["W12"]; !strings.Contains(l, " | anchors 1 | bib 1") {
		t.Errorf("W12 line %q, want anchors 1 | bib 1", l)
	}
	if l := works["W10"]; !strings.Contains(l, " | anchors 1 | bib 0") {
		t.Errorf("W10 line %q, want anchors 1 | bib 0", l)
	}
	// W12 has the extra bibliography link, so it now leads.
	if order := chainOrder(out); len(order) == 0 || order[0] != "W21" || order[1] != "W12" {
		t.Errorf("order %v, want W21 (2, cited 30) then W12 (1+1)", order)
	}

	doiRequests := 0
	for _, r := range rec.paths() {
		if q, _ := url.ParseQuery(strings.SplitN(r, "?", 2)[1]); strings.HasPrefix(q.Get("filter"), "doi:") {
			doiRequests++
			if want := "doi:https://doi.org/10.1000/x11|https://doi.org/10.1000/x30"; q.Get("filter") != want {
				t.Errorf("DOI filter %q, want %q", q.Get("filter"), want)
			}
		}
	}
	if doiRequests != 1 {
		t.Errorf("%d doi: requests, want 1: %v", doiRequests, rec.paths())
	}
}

func TestChainBibResolvesByTitle(t *testing.T) {
	fixtureStore(t)
	rec := chainFixture(t)
	bib := writeChainBib(t, `@article{thirty1985,
  author = {Tom Thirty},
  title = {Martingale methods in finance},
  year = {1985},
  doi = {10.1000/x30},
}

@article{marsh2001,
  author = {Mia Marsh},
  title = {Tidal marshes and the carbon cycle},
  year = {2001},
}
`)
	var err error
	out := captureStdout(t, func() { err = runChain([]string{"-short", "-bib", bib, "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\nbib: resolved 2 of 2 entries\ntotal: ") {
		t.Errorf("no bib line:\n%s", out)
	}
	// W22 cites W31, which only the title finds; W12 cites W30, found by DOI.
	if l := worksIn(out)["W22"]; !strings.Contains(l, " | bib 1") {
		t.Errorf("W22 line %q, want bib 1", l)
	}
	if l := worksIn(out)["W12"]; !strings.Contains(l, " | bib 1") {
		t.Errorf("W12 line %q, want bib 1", l)
	}
	// W31 cites W20, which cites no bibliography entry itself.
	if l := worksIn(out)["W20"]; !strings.Contains(l, " | bib 1") {
		t.Errorf("W20 line %q, want bib 1 (cited by the entry W31)", l)
	}
	var doiRequests, titleRequests int
	for _, r := range rec.paths() {
		q, _ := url.ParseQuery(strings.SplitN(r, "?", 2)[1])
		switch f := q.Get("filter"); {
		case strings.HasPrefix(f, "doi:"):
			doiRequests++
		case strings.HasPrefix(f, "title.search:"):
			titleRequests++
		}
	}
	if doiRequests != 1 || titleRequests != 1 {
		t.Errorf("%d doi: and %d title.search requests, want 1 and 1: %v", doiRequests, titleRequests, rec.paths())
	}
}

func TestChainSinceAppliesToCitingOnly(t *testing.T) {
	fixtureStore(t)
	rec := chainFixture(t)
	var err error
	captureStdout(t, func() { err = runChain([]string{"-short", "-since", "2020", "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	var citing, batches int
	for _, r := range rec.paths() {
		q, _ := url.ParseQuery(strings.SplitN(r, "?", 2)[1])
		filter := q.Get("filter")
		switch {
		case strings.HasPrefix(filter, "cites:"):
			citing++
			if !strings.HasSuffix(filter, ",from_publication_date:2020-01-01") {
				t.Errorf("citing request without -since: %s", r)
			}
		case strings.HasPrefix(filter, "openalex_id:"):
			batches++
			if strings.Contains(filter, "from_publication_date") {
				t.Errorf("reference batch with -since: %s", r)
			}
		}
	}
	if citing != 2 || batches != 2 {
		t.Errorf("%d citing and %d batch requests, want 2 and 2: %v", citing, batches, rec.paths())
	}
}

func TestChainUnknownAnchor(t *testing.T) {
	_, dir := fixtureStore(t)
	chainFixture(t)
	var err error
	var out string
	stderr := captureStderr(t, func() {
		out = captureStdout(t, func() { err = runChain([]string{"-short", "W99", "W2"}) })
	})
	if err != nil {
		t.Fatalf("one unknown anchor ended the run: %v", err)
	}
	if !strings.Contains(stderr, "anchor not found: W99") {
		t.Errorf("stderr %q lacks the unknown anchor", stderr)
	}
	if n := strings.Count(out, "anchor: "); n != 1 {
		t.Errorf("%d anchor lines, want 1:\n%s", n, out)
	}
	// W2 alone: W1 is no longer an anchor, so it is listed with the rest.
	if !strings.Contains(out, "total: 5, shown: 5\n") {
		t.Errorf("total from W2 alone should be 5:\n%s", out)
	}
	log := readEventLog(t, dir)
	if !strings.Contains(log, `"outcome":"openalex-unknown"`) || !strings.Contains(log, `"ref":"W99"`) {
		t.Errorf("no openalex-unknown event for W99:\n%s", log)
	}

	captureStderr(t, func() {
		captureStdout(t, func() { err = runChain([]string{"-short", "W98", "W99"}) })
	})
	if err == nil || !strings.Contains(err.Error(), "chain: no anchor found") {
		t.Errorf("every anchor unknown: err = %v", err)
	}
}

func TestChainTrailingFlag(t *testing.T) {
	err := runChain([]string{"W1", "-since", "2020"})
	if err == nil || !strings.Contains(err.Error(), "chain: flags go before the IDs") {
		t.Errorf("err = %v", err)
	}
}

func TestChainJSON(t *testing.T) {
	fixtureStore(t)
	chainFixture(t)
	bib := writeChainBib(t, "@article{x,\n  title = {Martingale methods in finance},\n  doi = {10.1000/x30},\n}\n")
	var err error
	out := captureStdout(t, func() { err = runChain([]string{"-json", "-short", "-n", "3", "-bib", bib, "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Anchors []struct {
			ID          string `json:"id"`
			Refs        int    `json:"refs"`
			Citing      int    `json:"citing"`
			CitingTotal int    `json:"citing_total"`
		} `json:"anchors"`
		Total int `json:"total"`
		Works []struct {
			ID      string `json:"id"`
			Anchors int    `json:"anchors"`
			Bib     *int   `json:"bib"`
		} `json:"works"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(got.Anchors) != 2 || got.Anchors[0].ID != "W1" || got.Anchors[0].Refs != 2 ||
		got.Anchors[0].Citing != 3 || got.Anchors[0].CitingTotal != 3 {
		t.Errorf("anchors %+v", got.Anchors)
	}
	if got.Total != 6 || len(got.Works) != 3 {
		t.Errorf("total %d, %d works, want 6 and 3 (-n 3)", got.Total, len(got.Works))
	}
	if len(got.Works) == 3 {
		w := got.Works[0]
		if w.ID != "W21" || w.Anchors != 2 || w.Bib == nil || *w.Bib != 0 {
			t.Errorf("first work %+v", w)
		}
		// W12 (1 anchor + 1 bib) and W11 (2 anchors) tie; W12 is cited more.
		if w := got.Works[1]; w.ID != "W12" {
			t.Errorf("second work %+v, want W12", w)
		}
	}
	if !strings.Contains(out, `"bib": {`) || !strings.Contains(out, `"resolved": 1`) || !strings.Contains(out, `"entries": 1`) {
		t.Errorf("no top-level bib object:\n%s", out)
	}
	if !strings.Contains(out, `"bib": 0`) {
		t.Errorf("a zero bib count must be written:\n%s", out)
	}

	// Without -bib no work has a bib member.
	out = captureStdout(t, func() { err = runChain([]string{"-json", "-short", "W1", "W2"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"bib"`) {
		t.Errorf("bib member without -bib:\n%s", out)
	}
}
