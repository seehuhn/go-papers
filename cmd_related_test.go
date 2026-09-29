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

// relatedWork is a work in the fixture server.
type relatedWork struct {
	id, doi, title, arxivPage string
	year, cited               int
	refs                      []string
}

func (w relatedWork) json() string {
	doi := "null"
	if w.doi != "" {
		doi = fmt.Sprintf("%q", "https://doi.org/"+w.doi)
	}
	refs := make([]string, len(w.refs))
	for i, r := range w.refs {
		refs[i] = fmt.Sprintf("%q", "https://openalex.org/"+r)
	}
	loc := "[]"
	if w.arxivPage != "" {
		loc = fmt.Sprintf(`[{"landing_page_url": %q}]`, w.arxivPage)
	}
	return fmt.Sprintf(`{"id": "https://openalex.org/%s", "doi": %s, "display_name": %q,
 "publication_year": %d, "type": "article", "cited_by_count": %d,
 "referenced_works": [%s], "locations": %s,
 "authorships": [{"author": {"display_name": "Ann Author"}}]}`,
		w.id, doi, w.title, w.year, w.cited, strings.Join(refs, ","), loc)
}

// relatedServer serves a small OpenAlex: works by ID, DOI and arXiv DOI,
// batches by openalex_id filter, citations by cites filter, and titles by
// title.search filter. It records the requests as "path?query".
type relatedServer struct {
	works  map[string]relatedWork // by ID
	byPath map[string]string      // "/works/doi:..." -> ID
	byName map[string]string      // title.search value -> ID
	citing map[string][]string    // work ID -> IDs of its citing works
	rec    *openAlexRecorder
}

func (fx *relatedServer) start(t *testing.T) {
	t.Helper()
	fx.rec = &openAlexRecorder{}
	list := func(ids []string) string {
		var parts []string
		for _, id := range ids {
			if w, ok := fx.works[id]; ok {
				parts = append(parts, w.json())
			}
		}
		return fmt.Sprintf(`{"meta": {"count": %d}, "results": [%s]}`, len(parts), strings.Join(parts, ","))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.rec.add(r.URL.Path + "?" + r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if id, ok := fx.byPath[r.URL.Path]; ok {
			w.Write([]byte(fx.works[id].json()))
			return
		}
		if r.URL.Path == "/works" {
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.HasPrefix(filter, "openalex_id:"):
				w.Write([]byte(list(strings.Split(strings.TrimPrefix(filter, "openalex_id:"), "|"))))
				return
			case strings.HasPrefix(filter, "cites:"):
				id, _, _ := strings.Cut(strings.TrimPrefix(filter, "cites:"), ",")
				w.Write([]byte(list(fx.citing[id])))
				return
			case strings.HasPrefix(filter, "title.search:"):
				var ids []string
				if id, ok := fx.byName[strings.TrimPrefix(filter, "title.search:")]; ok {
					ids = append(ids, id)
				}
				w.Write([]byte(list(ids)))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	overrideOpenAlex(t, srv.URL)
}

// newRelatedFixture returns the server for testdata/related.bib. The
// bibliography resolves to W1 (DOI), W2 (arXiv) and W3 (title); the
// entries with DOI 10.1000/E and the title "Delta results ..." do not
// resolve.
func newRelatedFixture(t *testing.T) *relatedServer {
	t.Helper()
	ws := []relatedWork{
		{id: "W1", doi: "10.1000/a", title: "Alpha theory of estimation", year: 2020, cited: 50,
			refs: []string{"W10", "W11", "W13", "W50"}},
		{id: "W2", title: "Bravo processes on a line", year: 2006, cited: 40,
			arxivPage: "https://arxiv.org/abs/math/0611001v2",
			refs:      []string{"W11", "W12", "W1", "W40"}},
		{id: "W3", doi: "10.1000/c", title: "Charlie estimators for sparse models", year: 2018, cited: 30,
			refs: []string{"W12", "W51"}},
		{id: "W10", doi: "10.1000/x10", title: "Ten", year: 1990, cited: 10},
		{id: "W11", doi: "10.1000/x11", title: "Eleven", year: 1991, cited: 11},
		{id: "W12", doi: "10.1000/x12", title: "Twelve", year: 1992, cited: 12},
		{id: "W13", doi: "10.1000/x13", title: "Thirteen", year: 1993, cited: 100},
		// Other versions of bibliography entries, under other IDs:
		{id: "W40", title: "Alpha theory of estimation", year: 2019, cited: 5},
		{id: "W50", doi: "10.1000/a", title: "Some retitled version", year: 2020, cited: 6},
		{id: "W51", title: "Another title entirely", year: 2007, cited: 7,
			arxivPage: "https://arxiv.org/abs/math/0611001v1"},
		{id: "W30", doi: "10.1000/x30", title: "Cites everything", year: 2023, cited: 3},
		{id: "W31", doi: "10.1000/x31", title: "Cites one", year: 2024, cited: 1},
		{id: "W99", title: "Unrelated matter", year: 2000, cited: 1},
	}
	fx := &relatedServer{
		works: map[string]relatedWork{},
		byPath: map[string]string{
			"/works/doi:10.1000/A":                      "W1",
			"/works/doi:10.48550/arXiv.math.PR/0611001": "W2",
		},
		byName: map[string]string{
			"Charlie estimators for sparse models": "W3",
			"Delta results that nobody indexed":    "W99",
		},
		citing: map[string][]string{
			"W1": {"W30", "W31", "W2"},
			"W2": {"W30", "W40"},
			"W3": {"W30", "W1"},
		},
	}
	for _, w := range ws {
		fx.works[w.id] = w
	}
	fx.start(t)
	return fx
}

// worksIn returns the work lines of related's plain output, by OpenAlex ID.
func worksIn(out string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if id, _, ok := strings.Cut(l, " | "); ok && !strings.HasPrefix(l, " ") {
			m[id] = l
		}
	}
	return m
}

func TestRelatedResolvesAllForms(t *testing.T) {
	_, dir := fixtureStore(t)
	newRelatedFixture(t)

	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "resolved: 3 of 5 entries" {
		t.Errorf("first line %q", lines[0])
	}
	if lines[1] != "unresolved: delta2019, echo2021" {
		t.Errorf("second line %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "total: ") {
		t.Errorf("third line %q, want the printWorks header", lines[2])
	}

	log := readEventLog(t, dir)
	if n := strings.Count(log, `"outcome":"openalex-unresolved"`); n != 2 {
		t.Errorf("%d openalex-unresolved events, want 2:\n%s", n, log)
	}
	for _, want := range []string{`"ref":"delta2019 Delta results that nobody indexed"`, `"ref":"echo2021 10.1000/E"`,
		`"command":"related"`, `"ref":"testdata/related.bib"`, `"source":"openalex"`} {
		if !strings.Contains(log, want) {
			t.Errorf("event log lacks %s:\n%s", want, log)
		}
	}
}

func TestRelatedCounts(t *testing.T) {
	newRelatedFixture(t)
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	if l := works["W11"]; !strings.Contains(l, "cited-by-yours 2") || strings.Contains(l, "cites-yours") {
		t.Errorf("W11 line %q, want cited-by-yours 2 only", l)
	}
	if l := works["W30"]; !strings.Contains(l, "cites-yours 3") {
		t.Errorf("W30 line %q, want cites-yours 3", l)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[3], "W30 | ") {
		t.Errorf("first work is %q, want W30", lines[3])
	}
	// W11 and W12 tie on 2/2 and are ranked by citation count (12 > 11),
	// after W30 (3); W13 (1 reference, 100 citations) comes after them.
	var order []string
	for _, l := range lines[3:] {
		if id, _, ok := strings.Cut(l, " | "); ok {
			order = append(order, id)
		}
	}
	want := []string{"W30", "W12", "W11", "W13", "W10", "W31"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("order %v, want %v", order, want)
	}
}

func TestRelatedDropsOtherVersion(t *testing.T) {
	newRelatedFixture(t)
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	for id, why := range map[string]string{
		"W40": "same title as a bibliography entry",
		"W50": "same DOI as a bibliography entry, in other case",
		"W51": "same arXiv ID as a bibliography entry, other archive class",
	} {
		if _, ok := works[id]; ok {
			t.Errorf("%s is listed, but has the %s", id, why)
		}
	}
	if len(works) == 0 {
		t.Errorf("no works listed:\n%s", out)
	}
}

func TestRelatedDropsCitedWorks(t *testing.T) {
	newRelatedFixture(t)
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	for _, id := range []string{"W1", "W2", "W3", "W99"} {
		if _, ok := works[id]; ok {
			t.Errorf("%s is listed:\n%s", id, out)
		}
	}
}

func TestRelatedLimitsAndSince(t *testing.T) {
	fx := newRelatedFixture(t)
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", "-n", "2", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	if got := worksIn(out); len(got) != 2 {
		t.Errorf("-n 2 listed %d works:\n%s", len(got), out)
	}

	out = captureStdout(t, func() { err = runRelated([]string{"-short", "-since", "2000", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	if _, ok := works["W30"]; !ok || len(works) != 2 {
		t.Errorf("-since 2000 lists %v, want W30 and W31 only:\n%s", works, out)
	}
	sawSince := false
	for _, r := range fx.rec.paths() {
		if q, _ := url.ParseQuery(strings.SplitN(r, "?", 2)[1]); strings.Contains(q.Get("filter"), "cites:") {
			sawSince = sawSince || strings.HasSuffix(q.Get("filter"), ",from_publication_date:2000-01-01")
		}
	}
	if !sawSince {
		t.Errorf("no citing request carried the -since filter: %v", fx.rec.paths())
	}
}

func TestRelatedJSON(t *testing.T) {
	newRelatedFixture(t)
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-json", "-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total      int      `json:"total"`
		Resolved   int      `json:"resolved"`
		Unresolved []string `json:"unresolved"`
		Works      []struct {
			ID         string `json:"id"`
			CitesYours int    `json:"cites_yours"`
		} `json:"works"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if got.Resolved != 3 || strings.Join(got.Unresolved, ",") != "delta2019,echo2021" {
		t.Errorf("resolved %d, unresolved %v", got.Resolved, got.Unresolved)
	}
	if len(got.Works) == 0 || got.Works[0].ID != "W30" || got.Works[0].CitesYours != 3 {
		t.Errorf("works %+v", got.Works)
	}
	if got.Total != len(got.Works) {
		t.Errorf("total %d, works %d", got.Total, len(got.Works))
	}
}

func TestRelatedLogsSummaryAndNoRefs(t *testing.T) {
	_, dir := fixtureStore(t)
	newRelatedFixture(t)
	var err error
	captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	log := readEventLog(t, dir)
	// W2 and W3 have references; strip them to see the miss.
	if strings.Contains(log, "openalex-no-refs") {
		t.Errorf("unexpected no-refs event:\n%s", log)
	}
	if !strings.Contains(log, `"outcome":"ok"`) || !strings.Contains(log, `"hits":6`) {
		t.Errorf("summary event missing:\n%s", log)
	}
}

func TestRelatedNoRefsLogsMiss(t *testing.T) {
	_, dir := fixtureStore(t)
	fx := newRelatedFixture(t)
	w := fx.works["W3"]
	w.refs = nil
	fx.works["W3"] = w
	var err error
	captureStdout(t, func() { err = runRelated([]string{"-short", "testdata/related.bib"}) })
	if err != nil {
		t.Fatal(err)
	}
	log := readEventLog(t, dir)
	if !strings.Contains(log, `"outcome":"openalex-no-refs"`) || !strings.Contains(log, `"ref":"charlie2018 W3"`) {
		t.Errorf("no-refs event missing:\n%s", log)
	}
}

func TestRelatedResolvesURLOnlyDOI(t *testing.T) {
	fx := newRelatedFixture(t)
	fx.byPath["/works/doi:10.1000/U"] = "W3"
	bib := filepath.Join(t.TempDir(), "u.bib")
	body := "@article{url1, title = {Something else}, url = {http://dx.doi.org/10.1000/U}}\n"
	if err := os.WriteFile(bib, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", bib}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "resolved: 1 of 1 entries\n") {
		t.Errorf("output %q", out)
	}
}

func TestRelatedNeedsOneFile(t *testing.T) {
	fixtureStore(t)
	for _, args := range [][]string{nil, {"a.bib", "b.bib"}, {"missing.bib"}} {
		err := runRelated(args)
		if err == nil || !strings.HasPrefix(err.Error(), "related:") {
			t.Errorf("args %v: error %v, want prefix related:", args, err)
		}
	}
}

// TestRelatedDropsOtherVersionByLookupCase covers the other side of the
// DOI case folding: the bibliography spells its DOI in lower case and
// OpenAlex spells the DOI of another version in upper case.
func TestRelatedDropsOtherVersionByLookupCase(t *testing.T) {
	fixtureStore(t)
	ws := []relatedWork{
		{id: "W60", doi: "10.1000/f", title: "Foxtrot methods", year: 2015, cited: 20,
			refs: []string{"W61", "W62"}},
		{id: "W61", doi: "10.1000/F", title: "A retitled Foxtrot", year: 2015, cited: 4},
		{id: "W62", doi: "10.1000/x62", title: "Sixty-two", year: 2010, cited: 9},
	}
	fx := &relatedServer{
		works:  map[string]relatedWork{},
		byPath: map[string]string{"/works/doi:10.1000/f": "W60"},
		byName: map[string]string{},
		citing: map[string][]string{},
	}
	for _, w := range ws {
		fx.works[w.id] = w
	}
	fx.start(t)

	bib := filepath.Join(t.TempDir(), "refs.bib")
	entry := "@article{foxtrot2015,\n  author = {Fay Foxtrot},\n  title = {Foxtrot methods},\n  year = {2015},\n  doi = {10.1000/f},\n}\n"
	if err := os.WriteFile(bib, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runRelated([]string{"-short", bib}) })
	if err != nil {
		t.Fatal(err)
	}
	works := worksIn(out)
	if _, ok := works["W61"]; ok {
		t.Errorf("W61 is listed, but has the same DOI as a bibliography entry, in other case:\n%s", out)
	}
	if _, ok := works["W62"]; !ok {
		t.Errorf("W62 is not listed:\n%s", out)
	}
}
