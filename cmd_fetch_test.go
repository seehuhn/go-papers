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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

// crossrefWorkResponse is a single-work Crossref response for
// Hoeffding's 1963 paper, shaped like the fixtures in
// internal/sources/crossref_test.go.
const crossrefWorkResponse = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1080/01621459.1963.10500830","type":"journal-article",
  "title":["Probability Inequalities for Sums of Bounded Random Variables"],
  "container-title":["Journal of the American Statistical Association"],
  "author":[{"given":"Wassily","family":"Hoeffding","sequence":"first"}],
  "volume":"58","issue":"301","page":"13-30",
  "ISSN":["0162-1459"],"publisher":"Informa UK Limited",
  "published":{"date-parts":[[1963,3]]}}}`

// crossrefAmbiguousSearchResponse is a search response with two hits whose
// scores are too close for the top hit to be auto-accepted.
const crossrefAmbiguousSearchResponse = `{"status":"ok","message-type":"work-list","message":{"items":[
  {"DOI":"10.1000/first","type":"journal-article","score":52.5,
   "title":["Some Ambiguous Title"],
   "container-title":["Journal of Ambiguity"],
   "author":[{"given":"Ann","family":"First"}],
   "published":{"date-parts":[[1999]]}},
  {"DOI":"10.1000/second","type":"journal-article","score":48.0,
   "title":["Some Ambiguous Title, Revisited"],
   "author":[{"given":"Bob","family":"Second"}],
   "published":{"date-parts":[[2001]]}}]}}`

// arxivResponseNoDOI is an arXiv API response for a record that carries no
// <arxiv:doi> element, so resolving it must not contact Crossref.
const arxivResponseNoDOI = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2412.05039v2</id>
    <title>A study of SPDEs in Greenland</title>
    <summary>We study stochastic partial differential equations.</summary>
    <published>2024-12-06T14:00:00Z</published>
    <author><name>Jochen Voß</name></author>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="math.PR"/>
    <category term="math.PR"/>
  </entry>
</feed>`

// gzipTarballBytes returns a gzipped tarball holding a minimal tex source,
// as served by the arXiv e-print endpoint.
func gzipTarballBytes(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"main.tex": `\documentclass{article}\begin{document}hello\end{document}`,
		"refs.bib": "@misc{x,}",
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fetchFixtureStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PAPER_STORE", dir)
	err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"email": "test@example.org"}`+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func overrideBases(t *testing.T, crossref, arxiv, unpaywall, zbmath, dblp string) {
	t.Helper()
	saved := []struct {
		p   *string
		old string
	}{{&crossrefBase, crossrefBase}, {&arxivBase, arxivBase},
		{&unpaywallBase, unpaywallBase}, {&zbmathBase, zbmathBase}, {&dblpBase, dblpBase}}
	t.Cleanup(func() {
		for _, s := range saved {
			*s.p = s.old
		}
	})
	if crossref != "" {
		crossrefBase = crossref
	}
	if arxiv != "" {
		arxivBase = arxiv
	}
	if unpaywall != "" {
		unpaywallBase = unpaywall
	}
	if zbmath != "" {
		zbmathBase = zbmath
	}
	if dblp != "" {
		dblpBase = dblp
	}
}

func TestFetchDOIWithOA(t *testing.T) {
	dir := fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4 the paper")
	}))
	t.Cleanup(pdfSrv.Close)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	upwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"is_oa":true,"best_oa_location":{"url_for_pdf":%q,"host_type":"repository"}}`, pdfSrv.URL)
	}))
	t.Cleanup(upwSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", upwSrv.URL, "", "")

	err := runFetch([]string{"10.1080/01621459.1963.10500830"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := store.Open("")
	p, err := s.Load("hoeffding_1963")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "draft" || p.Holdings != "published" {
		t.Errorf("status/holdings = %q/%q", p.Status, p.Holdings)
	}
	if _, err := os.Stat(filepath.Join(dir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("published.pdf should have been downloaded")
	}
}

func TestFetchDOIWithoutOA(t *testing.T) {
	fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	upwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"is_oa":false,"best_oa_location":null}`)
	}))
	t.Cleanup(upwSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", upwSrv.URL, "", "")

	err := runFetch([]string{"10.1080/01621459.1963.10500830"})
	if err == nil {
		t.Fatal("no OA route: fetch must fail so the agent takes over")
	}
	for _, want := range []string{"hoeffding_1963", "doi.org/10.1080", "Hoeffding", "no open-access"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%s", want, err)
		}
	}
	s, _ := store.Open("")
	p, err := s.Load("hoeffding_1963")
	if err != nil || p.Holdings != "none" {
		t.Error("the metadata-only draft entry must still have been created")
	}
}

func TestFetchArxiv(t *testing.T) {
	dir := fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "e-print") {
			w.Write(gzipTarballBytes(t))
			return
		}
		io.WriteString(w, "%PDF-1.4 arxiv pdf")
	}))
	t.Cleanup(pdfSrv.Close)
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseNoDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	overrideBases(t, "", arxivSrv.URL, "", "", "")
	// downloads must hit the test server, not arxiv.org
	savedDL := arxivDownloadBase
	arxivDownloadBase = pdfSrv.URL
	t.Cleanup(func() { arxivDownloadBase = savedDL })

	if err := runFetch([]string{"arXiv:2412.05039v2"}); err != nil {
		t.Fatal(err)
	}
	s, _ := store.Open("")
	p, err := s.Load("voss_2024")
	if err != nil {
		t.Fatal(err)
	}
	if p.Holdings != "preprint" || p.Arxiv == nil {
		t.Errorf("holdings/arxiv = %q/%+v", p.Holdings, p.Arxiv)
	}
	if _, err := os.Stat(filepath.Join(dir, "voss_2024", "arxiv-2412.05039v2.pdf")); err != nil {
		t.Error("versioned arXiv PDF missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "voss_2024", "arxiv-2412.05039v2", "main.tex")); err != nil {
		t.Error("extracted tex source missing")
	}
}

// arxivResponseWithDOI is an arXiv API response for a record that names a
// published version, so resolving it must consider that DOI.
const arxivResponseWithDOI = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom">
  <entry>
    <id>http://arxiv.org/abs/2412.05039v2</id>
    <title>A study of SPDEs in Greenland</title>
    <summary>We study stochastic partial differential equations.</summary>
    <published>2024-12-06T14:00:00Z</published>
    <author><name>Jochen Voß</name></author>
    <arxiv:doi xmlns:arxiv="http://arxiv.org/schemas/atom">10.1234/example.doi</arxiv:doi>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="math.PR"/>
    <category term="math.PR"/>
  </entry>
</feed>`

// TestFetchArxivEntryPassesCheck is the regression test for the ruling
// that the bibtex eprint field holds the bare arXiv ID: with a version
// suffix, every fetched arXiv entry trips check's eprint/arxiv.id
// consistency rule the moment it is created.
func TestFetchArxivEntryPassesCheck(t *testing.T) {
	fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "e-print") {
			w.Write(gzipTarballBytes(t))
			return
		}
		io.WriteString(w, "%PDF-1.4 arxiv pdf")
	}))
	t.Cleanup(pdfSrv.Close)
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseNoDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	overrideBases(t, "", arxivSrv.URL, "", "", "")
	savedDL := arxivDownloadBase
	arxivDownloadBase = pdfSrv.URL
	t.Cleanup(func() { arxivDownloadBase = savedDL })

	if err := runFetch([]string{"arXiv:2412.05039v2"}); err != nil {
		t.Fatal(err)
	}
	s, _ := store.Open("")
	p, err := s.Load("voss_2024")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Bibtex.Fields["eprint"]; got != "2412.05039" {
		t.Errorf("eprint = %q, want the bare ID", got)
	}

	var checkErr error
	out := captureStdout(t, func() { checkErr = runCheck(nil) })
	if checkErr != nil {
		t.Errorf("a freshly fetched arXiv entry must pass check, got %v\n%s", checkErr, out)
	}
}

// TestFetchArxivPDFFailureReportsContext pins the agent-fallback contract
// on the arXiv branch: once the entry is created, a failed download must
// hand over everything fetch has learned.
func TestFetchArxivPDFFailureReportsContext(t *testing.T) {
	fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>Please sign in</html>")
	}))
	t.Cleanup(pdfSrv.Close)
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseNoDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	overrideBases(t, "", arxivSrv.URL, "", "", "")
	savedDL := arxivDownloadBase
	arxivDownloadBase = pdfSrv.URL
	t.Cleanup(func() { arxivDownloadBase = savedDL })

	err := runFetch([]string{"arXiv:2412.05039v2"})
	if err == nil {
		t.Fatal("a failed PDF download must be reported")
	}
	for _, want := range []string{"voss_2024", "arxiv.org/abs/2412.05039v2", "not a PDF", "Voß"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%s", want, err)
		}
	}
	s, _ := store.Open("")
	p, err := s.Load("voss_2024")
	if err != nil || p.Holdings != "none" {
		t.Errorf("the draft entry must survive the failed download: %+v, %v", p, err)
	}
}

// TestFetchArxivDuplicateDOI covers the duplicate check on the DOI the
// arXiv record teaches us: a paper already in the store under its
// published DOI must not gain a second entry via its preprint.
func TestFetchArxivDuplicateDOI(t *testing.T) {
	fetchFixtureStore(t)
	s, _ := store.Open("")
	err := s.Save(&store.Paper{Key: "voss_2024", Status: "clean", Holdings: "published",
		DOI: "10.1234/example.doi",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Vo{\\ss}, Jochen", "title": "T", "journal": "J", "year": "2024"}}})
	if err != nil {
		t.Fatal(err)
	}
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseWithDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	// A known duplicate must be caught before the Crossref merge request
	// and before any download; both servers fail the test if contacted,
	// which also keeps a regression from reaching the live services.
	guardSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a known duplicate must not be looked up or downloaded: %s", r.URL)
	}))
	t.Cleanup(guardSrv.Close)
	overrideBases(t, guardSrv.URL, arxivSrv.URL, "", "", "")
	savedDL := arxivDownloadBase
	arxivDownloadBase = guardSrv.URL
	t.Cleanup(func() { arxivDownloadBase = savedDL })

	err = runFetch([]string{"arXiv:2412.05039v2"})
	if err == nil || !strings.Contains(err.Error(), "voss_2024") {
		t.Errorf("the published DOI of an arXiv record must be checked too, got %v", err)
	}
	if keys, _ := s.Keys(); len(keys) != 1 {
		t.Errorf("no second entry may be created, found %v", keys)
	}
}

// TestAttachDownloadNeverReturnsNilPaper pins the contract its callers
// rely on: however badly Attach goes, the returned paper can still be
// used to build the hand-off message. It used to return nil when the
// reload failed, which turned a hand-off into a panic.
func TestAttachDownloadNeverReturnsNilPaper(t *testing.T) {
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4 the paper")
	}))
	t.Cleanup(pdfSrv.Close)

	newFetcher := func(t *testing.T) *fetcher {
		t.Helper()
		s, err := store.Open("")
		if err != nil {
			t.Fatal(err)
		}
		return &fetcher{store: s, api: pdfSrv.Client(), dl: pdfSrv.Client(), now: time.Now()}
	}
	paper := func() *store.Paper {
		return &store.Paper{Key: "hoeffding_1963", Status: "draft", Holdings: "none",
			Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
				"author": "Hoeffding, Wassily", "title": "T", "journal": "J", "year": "1963"}}}
	}

	t.Run("reload succeeds", func(t *testing.T) {
		dir := fetchFixtureStore(t)
		f := newFetcher(t)
		p := paper()
		if err := f.store.Save(p); err != nil {
			t.Fatal(err)
		}
		// A directory where the file should go: Attach refuses to overwrite.
		if err := os.MkdirAll(filepath.Join(dir, p.Key, "published.pdf"), 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := f.attachDownload(p, pdfSrv.URL, "published.pdf", "unpaywall")
		if err == nil {
			t.Fatal("Attach should have refused the existing destination")
		}
		if got == nil || got.Key != "hoeffding_1963" {
			t.Errorf("attachDownload must return a usable paper, got %+v", got)
		}
	})

	t.Run("reload also fails", func(t *testing.T) {
		dir := fetchFixtureStore(t)
		f := newFetcher(t)
		p := paper()
		// The destination is in the way and the directory holds no
		// paper.json, so the reload after the failed Attach fails too.
		if err := os.MkdirAll(filepath.Join(dir, p.Key, "published.pdf"), 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := f.attachDownload(p, pdfSrv.URL, "published.pdf", "unpaywall")
		if err == nil {
			t.Fatal("Attach should have refused the existing destination")
		}
		if got == nil || got.Key != "hoeffding_1963" {
			t.Errorf("attachDownload must return a usable paper even when the reload fails, got %+v", got)
		}
		if !strings.Contains(err.Error(), "reloading") {
			t.Errorf("the error should record the failed reload, got %v", err)
		}
	})
}

func TestFetchFreeTextAmbiguous(t *testing.T) {
	fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefAmbiguousSearchResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	zbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":[]}`)
	}))
	t.Cleanup(zbSrv.Close)
	dblpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":{"hits":{}}}`)
	}))
	t.Cleanup(dblpSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", zbSrv.URL, dblpSrv.URL)

	err := runFetch([]string{"some ambiguous title"})
	if err == nil || !strings.Contains(err.Error(), "re-run") {
		t.Errorf("ambiguous free text must fail with candidates and re-run instructions, got %v", err)
	}
	s, _ := store.Open("")
	keys, _ := s.Keys()
	if len(keys) != 0 {
		t.Errorf("no entry may be created on ambiguity, found %v", keys)
	}
}

// crossrefClearSearchResponse is a search response whose top hit
// outscores the runner-up by more than the auto-accept margin.
const crossrefClearSearchResponse = `{"status":"ok","message-type":"work-list","message":{"items":[
  {"DOI":"10.1080/01621459.1963.10500830","type":"journal-article","score":95.5,
   "title":["Probability Inequalities for Sums of Bounded Random Variables"],
   "author":[{"given":"Wassily","family":"Hoeffding"}],
   "published":{"date-parts":[[1963]]}},
  {"DOI":"10.2307/2282952","type":"journal-article","score":40.1,
   "title":["Something Else Entirely"],
   "published":{"date-parts":[[1964]]}}]}}`

// TestFetchFreeTextAccepted covers the other half of behavior branch 3:
// a clear top hit whose year the query confirms is taken without asking,
// and resolution then continues as if the DOI had been given.
func TestFetchFreeTextAccepted(t *testing.T) {
	dir := fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4 the paper")
	}))
	t.Cleanup(pdfSrv.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("/works", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefClearSearchResponse)
	})
	mux.HandleFunc("/works/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	})
	crossrefSrv := httptest.NewServer(mux)
	t.Cleanup(crossrefSrv.Close)
	upwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"is_oa":true,"best_oa_location":{"url_for_pdf":%q,"host_type":"repository"}}`, pdfSrv.URL)
	}))
	t.Cleanup(upwSrv.Close)
	// An accepted hit must not fall through to the candidate providers;
	// these stand in for zbMATH and DBLP so that a regression fails the
	// test here rather than reaching out to the live services.
	candSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("candidate search must not run after an accepted hit: %s", r.URL)
	}))
	t.Cleanup(candSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", upwSrv.URL, candSrv.URL, candSrv.URL)

	err := runFetch([]string{"Hoeffding", "probability", "inequalities", "1963"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("an accepted free-text hit must be fetched like a DOI")
	}
}

// TestFetchFreeTextNoYear pins the year condition as vacuous: when the
// query names no year there is nothing to contradict, so the score ratio
// alone decides and a clear top hit is still accepted.
func TestFetchFreeTextNoYear(t *testing.T) {
	dir := fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4 the paper")
	}))
	t.Cleanup(pdfSrv.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("/works", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefClearSearchResponse)
	})
	mux.HandleFunc("/works/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	})
	crossrefSrv := httptest.NewServer(mux)
	t.Cleanup(crossrefSrv.Close)
	upwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"is_oa":true,"best_oa_location":{"url_for_pdf":%q,"host_type":"repository"}}`, pdfSrv.URL)
	}))
	t.Cleanup(upwSrv.Close)
	candSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("candidate search must not run after an accepted hit: %s", r.URL)
	}))
	t.Cleanup(candSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", upwSrv.URL, candSrv.URL, candSrv.URL)

	if err := runFetch([]string{"Hoeffding probability inequalities"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("a clear top hit must be accepted even when the query names no year")
	}
}

// TestFetchFreeTextYearMismatch pins the other side of the same rule: a
// year in the query that contradicts the top hit blocks the auto-accept,
// however clear the scores are.
func TestFetchFreeTextYearMismatch(t *testing.T) {
	fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefClearSearchResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	zbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":[]}`)
	}))
	t.Cleanup(zbSrv.Close)
	dblpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":{"hits":{}}}`)
	}))
	t.Cleanup(dblpSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", zbSrv.URL, dblpSrv.URL)

	err := runFetch([]string{"Hoeffding probability inequalities 1994"})
	if err == nil || !strings.Contains(err.Error(), "re-run") {
		t.Errorf("a year that contradicts the top hit must block acceptance, got %v", err)
	}
	s, _ := store.Open("")
	if keys, _ := s.Keys(); len(keys) != 0 {
		t.Errorf("no entry may be created, found %v", keys)
	}
}

func TestFetchDuplicate(t *testing.T) {
	fetchFixtureStore(t)
	s, _ := store.Open("")
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "T", "journal": "J", "year": "1963"}}})
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "")

	err := runFetch([]string{"10.1080/01621459.1963.10500830"})
	if err == nil || !strings.Contains(err.Error(), "hoeffding_1963") {
		t.Errorf("duplicate DOI must name the existing key, got %v", err)
	}
}

// TestFetchDryRun covers behavior branch 4: -dry-run resolves and reports
// without writing to the store or downloading anything. The Unpaywall
// server would hand out a PDF URL, but the PDF server fails the test if it
// is ever contacted.
func TestFetchDryRun(t *testing.T) {
	dir := fetchFixtureStore(t)
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("dry run must not download anything")
	}))
	t.Cleanup(pdfSrv.Close)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	upwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"is_oa":true,"best_oa_location":{"url_for_pdf":%q,"host_type":"repository"}}`, pdfSrv.URL)
	}))
	t.Cleanup(upwSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", upwSrv.URL, "", "")

	var out string
	runErr := error(nil)
	out = captureStdout(t, func() {
		runErr = runFetch([]string{"-dry-run", "10.1080/01621459.1963.10500830"})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, want := range []string{"hoeffding_1963", "published.pdf"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run report should mention %q, got:\n%s", want, out)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("dry run must not write to the store, found %v (%v)", entries, err)
	}
}

func TestFetchLogsEvent(t *testing.T) {
	dir := fetchFixtureStore(t)
	s, _ := store.Open("")
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "T", "journal": "J", "year": "1963"}}})

	_ = runFetch([]string{"10.1080/01621459.1963.10500830"}) // duplicate -> error, still logged

	files, _ := filepath.Glob(filepath.Join(dir, "events", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("want one events file, got %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"outcome":"duplicate"`) {
		t.Errorf("events file should record the duplicate outcome:\n%s", data)
	}
}
