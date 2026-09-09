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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/pdfid/pdfidtest"
	"seehuhn.de/go/paper/internal/store"
)

// makeIngestPDF writes a one-page PDF. A non-empty doi puts a
// "DOI: <doi>" line in the body so the file identifies via tier 2 (text
// regex). Empty title and doi produce a text-free PDF (the
// unidentifiable/scanned case).
func makeIngestPDF(t *testing.T, path, title, doi string) {
	t.Helper()
	var lines []string
	if doi != "" {
		lines = []string{title, "DOI: " + doi}
	}
	pdfidtest.MakePDF(t, path, title, "Test Author", lines, []float64{24, 10})
}

// guardBases points every bibliographic-search service at a server that
// fails the test if it is contacted. A test whose files must resolve
// locally, without any title search or download, uses it, so that
// reaching one of those services is a failure by construction rather
// than something the fixtures happen to avoid.
//
// The handle resolver is the one exception: tier 2 now confirms a DOI
// extracted from a PDF's page text through it before accepting the DOI
// (see pdfid.Config.ValidateDOI), so -into's identity check legitimately
// consults it even though nothing else here should be reached.
// confirmingHandleServer reports every DOI as existing, which is all
// these tests need - they are not testing handle-existence semantics.
func guardBases(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no online service may be contacted here: %s", r.URL)
	}))
	t.Cleanup(srv.Close)
	overrideBases(t, srv.URL, srv.URL, srv.URL, srv.URL, srv.URL, confirmingHandleServer(t), "")
}

// pickupTitleBases points crossref at a stub that reports no hits, and
// every other service at a server that fails the test if contacted. A PDF
// with a title but no DOI or arXiv stamp always reaches tier 3, which
// falls back to the Info dictionary's title and asks crossref about it
// even when there is no page text to search from (see pdfid's tier3); a
// pickup test that matches candidates by title alone, rather than by DOI,
// needs crossref to answer rather than to refuse.
func pickupTitleBases(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"ok","message-type":"work-list","message":{"items":[]}}`)
	}))
	t.Cleanup(srv.Close)
	overrideBases(t, srv.URL, "", "", "", "", "", "")
}

// assertUntouched checks that an entry gained nothing from a run that was
// supposed to refuse the file: holdings unchanged, no version recorded.
func assertUntouched(t *testing.T, s *store.Store, key string) {
	t.Helper()
	p, err := s.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	if p.Holdings != "none" {
		t.Errorf("holdings = %q, want the entry left untouched", p.Holdings)
	}
	if len(p.Versions) != 0 {
		t.Errorf("versions = %v, want the entry left untouched", p.Versions)
	}
}

func TestIngestIntoNeedsExactlyOne(t *testing.T) {
	fetchFixtureStore(t)
	guardBases(t)
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "T", "journal": "J", "year": "1963"}}})
	dir := t.TempDir()
	a := filepath.Join(dir, "a.pdf")
	b := filepath.Join(dir, "b.pdf")
	makeIngestPDF(t, a, "Paper A", "10.1/a")
	makeIngestPDF(t, b, "Paper B", "10.1/b")

	err := runIngest([]string{"-into", "hoeffding_1963", a, b})
	if err == nil || !strings.Contains(err.Error(), "a.pdf") || !strings.Contains(err.Error(), "b.pdf") {
		t.Errorf("two survivors must error listing both, got %v", err)
	}
	for _, f := range []string{a, b} {
		if _, statErr := os.Stat(f); statErr != nil {
			t.Errorf("%s must be left in place", f)
		}
	}
	assertUntouched(t, s, "hoeffding_1963")
}

func TestIngestIntoVerifiesIdentity(t *testing.T) {
	fetchFixtureStore(t)
	guardBases(t)
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "JASA", "year": "1963"}}})
	f := filepath.Join(t.TempDir(), "wrong.pdf")
	makeIngestPDF(t, f, "A Completely Different Paper", "10.9999/mismatch")

	err := runIngest([]string{"-into", "hoeffding_1963", f})
	if err == nil || !strings.Contains(err.Error(), "10.9999/mismatch") {
		t.Errorf("mismatched file must error naming what the PDF looks like, got %v", err)
	}
	if _, statErr := os.Stat(f); statErr != nil {
		t.Error("rejected file must be left in place")
	}
	assertUntouched(t, s, "hoeffding_1963")
}

// TestIngestIntoRecordsMissingDOI pins that a file's DOI is recorded onto
// an entry that lacks one: the file is verified by title (the entry has
// no DOI to compare against), and the attach must not merely succeed but
// also record the DOI it found, with a log entry saying so.
func TestIngestIntoRecordsMissingDOI(t *testing.T) {
	fetchFixtureStore(t)
	guardBases(t)
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "JASA", "year": "1963"}}})
	f := filepath.Join(t.TempDir(), "hoeffding.pdf")
	makeIngestPDF(t, f, "Probability inequalities for sums of bounded random variables",
		"10.1080/01621459.1963.10500830")

	if err := runIngest([]string{"-into", "hoeffding_1963", f}); err != nil {
		t.Fatal(err)
	}

	p := loadEntry(t, s.Root, "hoeffding_1963")
	if p.DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("DOI = %q, want the file's DOI recorded", p.DOI)
	}
	found := false
	for _, l := range p.Log {
		if l.Action == "identifiers" && strings.Contains(l.Detail, "10.1080/01621459.1963.10500830") {
			found = true
		}
	}
	if !found {
		t.Errorf("log = %+v, want an identifiers entry naming the recorded DOI", p.Log)
	}
}

// TestIngestIntoDOIConflictRefusesAttach pins that a file whose DOI
// genuinely disagrees with the entry's already-recorded (non-arXiv) DOI
// is refused, even though its title matches: a real identifier conflict
// must not be silently attached on title agreement alone.
func TestIngestIntoDOIConflictRefusesAttach(t *testing.T) {
	fetchFixtureStore(t)
	guardBases(t)
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "JASA", "year": "1963"}}})
	f := filepath.Join(t.TempDir(), "hoeffding.pdf")
	makeIngestPDF(t, f, "Probability inequalities for sums of bounded random variables",
		"10.9999/conflicting-doi")

	err := runIngest([]string{"-into", "hoeffding_1963", f})
	if err == nil ||
		!strings.Contains(err.Error(), "10.9999/conflicting-doi") ||
		!strings.Contains(err.Error(), "10.1080/01621459.1963.10500830") {
		t.Errorf("conflicting DOI must error naming both, got %v", err)
	}
	if _, statErr := os.Stat(f); statErr != nil {
		t.Error("rejected file must be left in place")
	}
	assertUntouched(t, s, "hoeffding_1963")
}

func TestIngestBatchCreatesEntries(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse) // always the Hoeffding record
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", confirmingHandleServer(t), "")
	f := filepath.Join(t.TempDir(), "paper.pdf")
	makeIngestPDF(t, f, "Probability inequalities", "10.1080/01621459.1963.10500830")

	if err := runIngest([]string{f}); err != nil {
		t.Fatal(err)
	}
	s := openConfiguredStore(t)
	p, err := s.Load("hoeffding_1963")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "draft" || p.Holdings != "published" {
		t.Errorf("status/holdings = %q/%q", p.Status, p.Holdings)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("file should have been moved into the store")
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Error("source file must be moved away")
	}
}

// crossrefSparseWorkResponse is the Hoeffding record with the
// bibliographic fields a publisher's PRISM packet also carries
// (container-title, volume, issue, page) left out, so that a gap filled
// from PRISM is visible.
const crossrefSparseWorkResponse = `{"status":"ok","message-type":"work","message":{
  "DOI":"10.1080/01621459.1963.10500830","type":"journal-article",
  "title":["Probability Inequalities for Sums of Bounded Random Variables"],
  "author":[{"given":"Wassily","family":"Hoeffding","sequence":"first"}],
  "published":{"date-parts":[[1963,3]]}}}`

// TestIngestFillsGapsFromPrism is the end-to-end check of the typed XMP
// path: a PDF whose only identifier is a prism:doi, written as a DOI
// proxy URL the way a PRISM 2.0-era producer would.
//
// Two things must happen. The DOI must reach Crossref normalized - the
// proxy URL sent verbatim would 404 and turn an identified paper into an
// unidentified one - and the bibliographic fields Crossref left out must
// be filled from the PRISM packet rather than left for the polish pass.
func TestIngestFillsGapsFromPrism(t *testing.T) {
	fetchFixtureStore(t)

	var askedFor string
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		askedFor = r.URL.Path
		io.WriteString(w, crossrefSparseWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", "", "")

	f := filepath.Join(t.TempDir(), "prism.pdf")
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS20, map[string]string{
		"doi":             "https://doi.org/10.1080/01621459.1963.10500830",
		"publicationName": "Journal of the American Statistical Association",
		"issn":            "0162-1459",
		"volume":          "58",
		"number":          "301",
		"startingPage":    "13",
		"endingPage":      "30",
	})
	pdfidtest.MakePDF(t, f, "", "", []string{"body text without any identifier"}, nil,
		pdfidtest.WithXMP(packet))

	if err := runIngest([]string{f}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(askedFor, "10.1080/01621459.1963.10500830") ||
		strings.Contains(askedFor, "doi.org") {
		t.Errorf("crossref was asked for %q, want the bare DOI", askedFor)
	}

	s := openConfiguredStore(t)
	p, err := s.Load("hoeffding_1963")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"journal": "Journal of the American Statistical Association",
		"volume":  "58",
		"number":  "301",
		"pages":   "13--30",
		"issn":    "0162-1459",
	}
	for k, v := range want {
		if p.Bibtex.Fields[k] != v {
			t.Errorf("field %s = %q, want %q from the PRISM packet", k, p.Bibtex.Fields[k], v)
		}
	}
}

func TestIngestDOIOverride(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", "", "")
	f := filepath.Join(t.TempDir(), "scan.pdf")
	makeIngestPDF(t, f, "", "") // no text, unidentifiable on its own

	err := runIngest([]string{"-doi", "10.1080/01621459.1963.10500830", f})
	if err != nil {
		t.Fatal(err)
	}
	s := openConfiguredStore(t)
	if _, err := s.Load("hoeffding_1963"); err != nil {
		t.Error("entry should have been created from the override DOI")
	}
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("file should have been moved into the store")
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Error("source file must be moved away")
	}
}

// TestIngestDOIOverrideFillsGapsFromPrism checks that -doi still runs
// metadata extraction and fills gaps in the drafted entry from the PDF's
// PRISM packet, the same way the normal identification path does (see
// TestIngestFillsGapsFromPrism). -doi trusts the given DOI absolutely -
// there is no text in the fixture body for identification to find - but
// the file is in hand, so its XMP pages/ISSN must not be forfeited.
func TestIngestDOIOverrideFillsGapsFromPrism(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefSparseWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", "", "")

	f := filepath.Join(t.TempDir(), "prism-override.pdf")
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS20, map[string]string{
		"issn":         "0162-1459",
		"startingPage": "13",
		"endingPage":   "30",
	})
	pdfidtest.MakePDF(t, f, "", "", nil, nil, pdfidtest.WithXMP(packet)) // no text: -doi must not need it

	err := runIngest([]string{"-doi", "10.1080/01621459.1963.10500830", f})
	if err != nil {
		t.Fatal(err)
	}
	s := openConfiguredStore(t)
	p, err := s.Load("hoeffding_1963")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"pages": "13--30",
		"issn":  "0162-1459",
	}
	for k, v := range want {
		if p.Bibtex.Fields[k] != v {
			t.Errorf("field %s = %q, want %q from the PRISM packet", k, p.Bibtex.Fields[k], v)
		}
	}
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("file should have been moved into the store")
	}
}

func TestIngestUnidentifiable(t *testing.T) {
	fetchFixtureStore(t)
	guardBases(t)
	f := filepath.Join(t.TempDir(), "scan.pdf")
	makeIngestPDF(t, f, "", "")

	err := runIngest([]string{f})
	if err == nil || !strings.Contains(err.Error(), "OCR") {
		t.Errorf("text-free PDF must report the scanned case, got %v", err)
	}
	if _, statErr := os.Stat(f); statErr != nil {
		t.Error("unidentified file must be left in place")
	}
	s := openConfiguredStore(t)
	keys, _ := s.Keys()
	if len(keys) != 0 {
		t.Errorf("no entry may be created, found %v", keys)
	}
}

// TestIngestBatchPartialFailure pins the partial-failure semantics of a
// batch run: one file that cannot be identified does not stop the others,
// the successes are reported on stdout as they happen, and the closing
// error accounts for every file that was left behind.
func TestIngestBatchPartialFailure(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", confirmingHandleServer(t), "")
	dir := t.TempDir()
	bad := filepath.Join(dir, "scan.pdf")
	good := filepath.Join(dir, "paper.pdf")
	makeIngestPDF(t, bad, "", "") // no text, unidentifiable
	makeIngestPDF(t, good, "Probability inequalities", "10.1080/01621459.1963.10500830")

	// The unidentifiable file comes first, so the run has to carry on past
	// a failure to reach the good one.
	var err error
	out := captureStdout(t, func() { err = runIngest([]string{bad, good}) })

	if err == nil || !strings.Contains(err.Error(), "1 of 2") || !strings.Contains(err.Error(), bad) {
		t.Errorf("the closing error must account for the file left behind, got %v", err)
	}
	if !strings.Contains(out, "ingested "+good) {
		t.Errorf("the successful file must be reported on stdout, got:\n%s", out)
	}

	s := openConfiguredStore(t)
	if _, loadErr := s.Load("hoeffding_1963"); loadErr != nil {
		t.Errorf("the identifiable file must still have been ingested: %v", loadErr)
	}
	if _, statErr := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); statErr != nil {
		t.Error("the identifiable file should have been moved into the store")
	}
	if _, statErr := os.Stat(good); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("the ingested file must be moved away")
	}
	if _, statErr := os.Stat(bad); statErr != nil {
		t.Error("the unidentified file must be left in place")
	}
	if keys, _ := s.Keys(); len(keys) != 1 {
		t.Errorf("only the identifiable file may create an entry, found %v", keys)
	}
}

// TestIngestBatchSurvivesStatFailure pins that a path which cannot even be
// stat'ed (dangling symlink, permissions, an unmaterialized cloud
// placeholder) is a per-file failure, not a reason to abort the batch.
func TestIngestBatchSurvivesStatFailure(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "", confirmingHandleServer(t), "")

	good := filepath.Join(t.TempDir(), "good.pdf")
	makeIngestPDF(t, good, "Probability inequalities", "10.1080/01621459.1963.10500830")
	missing := filepath.Join(t.TempDir(), "does-not-exist.pdf")

	out := captureStdout(t, func() {
		err := runIngest([]string{missing, good})
		if err == nil || !strings.Contains(err.Error(), "does-not-exist.pdf") {
			t.Errorf("error must name the bad path, got %v", err)
		}
	})
	if !strings.Contains(out, "ingested") {
		t.Errorf("the good file must still be ingested, stdout:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("good file should be in the store despite the bad sibling")
	}
}

// TestIngestBatchArxiv covers the other half of behavior branch 3: a file
// carrying an arXiv stamp is resolved through the arXiv API and attached
// under its version-qualified name, without any download.
func TestIngestBatchArxiv(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseNoDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	// Ingest already holds the file, so nothing may be downloaded; this
	// server fails the test if the arXiv download route is used at all.
	guardSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("ingest must not download anything: %s", r.URL)
	}))
	t.Cleanup(guardSrv.Close)
	overrideBases(t, guardSrv.URL, arxivSrv.URL, "", "", "", "", "")
	savedDL := arxivDownloadBase
	arxivDownloadBase = guardSrv.URL
	t.Cleanup(func() { arxivDownloadBase = savedDL })

	f := filepath.Join(t.TempDir(), "preprint.pdf")
	pdfidtest.MakePDF(t, f, "", "", []string{
		"A study of SPDEs in Greenland",
		"arXiv:2412.05039v2 [math.PR] 6 Dec 2024",
	}, []float64{24, 10})

	if err := runIngest([]string{f}); err != nil {
		t.Fatal(err)
	}
	s := openConfiguredStore(t)
	p, err := s.Load("voss_2024")
	if err != nil {
		t.Fatal(err)
	}
	if p.Holdings != "preprint" || p.Arxiv == nil || p.Arxiv.ID != "2412.05039" {
		t.Errorf("holdings/arxiv = %q/%+v", p.Holdings, p.Arxiv)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "voss_2024", "arxiv-2412.05039v2.pdf")); err != nil {
		t.Error("file should have been moved in under its arXiv name")
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Error("source file must be moved away")
	}
}

// TestIngestIntoPreprintDiscoversArxiv pins the preprint-as-published
// invariant: an entry recorded from its published metadata (a DOI and a
// title, no arXiv ref) can still be handed a preprint PDF. Verification
// succeeds by title here — the file carries no DOI, and the entry has no
// arXiv ID to match against — but the file is an arXiv e-print, so it
// must be stored under its version-qualified arXiv name and the entry
// must record the arXiv identity the file disclosed. Filing it as
// published.pdf would claim a version of record the store does not hold.
func TestIngestIntoPreprintDiscoversArxiv(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	guardBases(t)
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "voss_2024", Status: "clean", Holdings: "none",
		DOI: "10.1000/spde-greenland",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Voss, Jochen",
			"title":   "A study of SPDEs in Greenland",
			"journal": "J. Fake Stud.", "year": "2024"}}})

	f := filepath.Join(t.TempDir(), "preprint.pdf")
	pdfidtest.MakePDF(t, f, "A study of SPDEs in Greenland", "Jochen Voss", []string{
		"A study of SPDEs in Greenland",
		"arXiv:2412.05039v2 [math.PR] 6 Dec 2024",
	}, []float64{24, 10})

	if err := runIngest([]string{"-into", "voss_2024", f}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(storeDir, "voss_2024", "arxiv-2412.05039v2.pdf")); err != nil {
		t.Error("the preprint must be stored under its version-qualified arXiv name")
	}
	if _, err := os.Stat(filepath.Join(storeDir, "voss_2024", "published.pdf")); err == nil {
		t.Error("a preprint must never be filed as the version of record")
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Error("attached file must be moved, not copied")
	}

	p, err := s.Load("voss_2024")
	if err != nil {
		t.Fatal(err)
	}
	if p.Holdings != "preprint" {
		t.Errorf("holdings = %q, want preprint", p.Holdings)
	}
	if p.Arxiv == nil || p.Arxiv.ID != "2412.05039" || p.Arxiv.Version != 2 {
		t.Errorf("arxiv = %+v, want the identity the file disclosed", p.Arxiv)
	}
}

// pickupFixture prepares a fixture store, points $HOME at a fresh temp
// directory with empty Desktop/ and Downloads/ subdirectories (so
// pickupDirs finds them without touching the real user's directories),
// and changes into a fresh workspace directory (the third directory the
// pickup scans). It returns the store root and the three directories.
func pickupFixture(t *testing.T) (root, downloads, desktop, work string) {
	t.Helper()
	root = fetchFixtureStore(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	downloads = filepath.Join(home, "Downloads")
	desktop = filepath.Join(home, "Desktop")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	work = t.TempDir()
	t.Chdir(work)
	return root, downloads, desktop, work
}

// writeAwaiting saves a holdings-"none" entry directly into the store at
// root, with a single log line recording its creation time. key must be
// of the form "surname_year", which the existing store keys already use;
// the surname and year are read back out of it for the entry's bibtex, so
// that the "still awaiting" report line has something to show.
func writeAwaiting(t *testing.T, root, key, title, when string) {
	t.Helper()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	surname, year, _ := strings.Cut(key, "_")
	if surname != "" {
		surname = strings.ToUpper(surname[:1]) + surname[1:]
	}
	p := &store.Paper{
		Key:      key,
		Status:   "draft",
		Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": surname + ", Test",
			"title":  title,
			"year":   year,
		}},
		Log: []store.LogEntry{{When: when, Action: "created", Detail: "created for test"}},
	}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
}

// loadEntry loads the entry at key from the store at root, failing the
// test if either the store or the entry cannot be opened.
func loadEntry(t *testing.T, root, key string) *store.Paper {
	t.Helper()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Load(key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPickupMovesUniqueMatch(t *testing.T) {
	pickupTitleBases(t)
	root, dl, _, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	pdf := filepath.Join(dl, "download.pdf")
	makeIngestPDF(t, pdf, "A study of widgets", "")
	now := time.Now()
	if err := os.Chtimes(pdf, now, now); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runIngest(nil); err != nil {
			t.Fatalf("pickup: %v", err)
		}
	})
	if !strings.Contains(out, "moved "+pdf+" -> smith_2020") {
		t.Fatalf("report: %s", out)
	}
	if _, err := os.Stat(pdf); !os.IsNotExist(err) {
		t.Fatalf("file not moved")
	}
	p := loadEntry(t, root, "smith_2020")
	if p.Holdings == "none" {
		t.Fatalf("holdings not updated")
	}
}

func TestPickupIgnoresOldFiles(t *testing.T) {
	pickupTitleBases(t)
	root, dl, _, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	pdf := filepath.Join(dl, "old.pdf")
	makeIngestPDF(t, pdf, "A study of widgets", "")
	old := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(pdf, old, old); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err == nil {
		t.Fatal("expected an error: nothing was moved")
	}
	if !strings.Contains(out, "still awaiting:") || !strings.Contains(out, "smith_2020") {
		t.Fatalf("report: %s", out)
	}
	if _, statErr := os.Stat(pdf); statErr != nil {
		t.Error("file older than every awaiting entry must be left in place")
	}
}

func TestPickupAmbiguousTwoEntries(t *testing.T) {
	pickupTitleBases(t)
	root, dl, _, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A shared title", "2026-09-01T10:00:00")
	writeAwaiting(t, root, "jones_2021", "A shared title", "2026-09-01T10:00:00")
	pdf := filepath.Join(dl, "download.pdf")
	makeIngestPDF(t, pdf, "A shared title", "")
	now := time.Now()
	if err := os.Chtimes(pdf, now, now); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err == nil {
		t.Fatal("expected an error: the file matches two entries")
	}
	if !strings.Contains(out, "ambiguous: ") || !strings.Contains(out, "smith_2020") || !strings.Contains(out, "jones_2021") {
		t.Fatalf("report: %s", out)
	}
	if _, statErr := os.Stat(pdf); statErr != nil {
		t.Error("ambiguous file must be left in place")
	}
}

func TestPickupAmbiguousTwoFiles(t *testing.T) {
	pickupTitleBases(t)
	root, dl, desktop, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	a := filepath.Join(dl, "a.pdf")
	b := filepath.Join(desktop, "b.pdf")
	makeIngestPDF(t, a, "A study of widgets", "")
	makeIngestPDF(t, b, "A study of widgets", "")
	now := time.Now()
	if err := os.Chtimes(a, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, now, now); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err == nil {
		t.Fatal("expected an error: the entry is matched by two files")
	}
	if !strings.Contains(out, "ambiguous: smith_2020 matched by") {
		t.Fatalf("report: %s", out)
	}
	for _, p := range []string{a, b} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("%s must be left in place", p)
		}
	}
}

// TestPickupAttachFailureReportsFailed pins that a uniquely matched
// candidate whose move fails after verification succeeds is reported as
// "failed: ", not "unidentified: " (the file was identified fine; only
// the move itself did not work). The destination file is pre-created so
// that store.Attach's own "already exists" check fails the move: a
// portable way to force this without relying on filesystem permissions.
func TestPickupAttachFailureReportsFailed(t *testing.T) {
	pickupTitleBases(t)
	root, dl, _, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	if err := os.WriteFile(filepath.Join(root, "smith_2020", "published.pdf"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(dl, "download.pdf")
	makeIngestPDF(t, pdf, "A study of widgets", "")
	now := time.Now()
	if err := os.Chtimes(pdf, now, now); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err == nil {
		t.Fatal("expected an error: the move itself must fail")
	}
	if !strings.Contains(out, "failed: "+pdf+" -> smith_2020") {
		t.Fatalf("report: %s", out)
	}
	if strings.Contains(out, "unidentified: "+pdf) {
		t.Errorf("an identified file whose move failed must not be reported as unidentified: %s", out)
	}
	if _, statErr := os.Stat(pdf); statErr != nil {
		t.Error("file must be left in place when the move fails")
	}
}

func TestPickupUnidentified(t *testing.T) {
	guardBases(t)
	root, dl, _, _ := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	bad := filepath.Join(dl, "x.pdf")
	if err := os.WriteFile(bad, []byte("this is not a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(bad, now, now); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err == nil {
		t.Fatal("expected an error: the file cannot be identified")
	}
	if !strings.Contains(out, "unidentified: ") || !strings.Contains(out, "record it by hand") {
		t.Fatalf("report: %s", out)
	}
	if _, statErr := os.Stat(bad); statErr != nil {
		t.Error("unidentified file must be left in place")
	}
}

// TestPickupIgnoresCleanHoldingsNone pins that a holdings-none entry
// whose status is "clean" is not treated as awaiting a file: a clean
// status means the entry passed "paper check" already, which happens
// for a deliberate metadata-only ("none" request) entry, not one still
// waiting on a download.
func TestPickupIgnoresCleanHoldingsNone(t *testing.T) {
	guardBases(t)
	root, _, _, _ := pickupFixture(t)

	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	p := &store.Paper{
		Key:      "smith_2020",
		Status:   "clean",
		Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Smith, Test", "title": "A study of widgets", "year": "2020"}},
		Log: []store.LogEntry{{When: "2026-09-01T10:00:00", Action: "created", Detail: "created for test"}},
	}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() { runErr = runIngest(nil) })
	if runErr != nil {
		t.Fatalf("pickup: %v", runErr)
	}
	if !strings.Contains(out, "no entry is awaiting a file") {
		t.Fatalf("report: %s", out)
	}
}

func TestPickupNothingAwaiting(t *testing.T) {
	guardBases(t)
	pickupFixture(t)

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if !strings.Contains(out, "no entry is awaiting a file") {
		t.Fatalf("report: %s", out)
	}
}

func TestPickupScansWorkspace(t *testing.T) {
	pickupTitleBases(t)
	root, _, _, work := pickupFixture(t)

	writeAwaiting(t, root, "smith_2020", "A study of widgets", "2026-09-01T10:00:00")
	pdf := filepath.Join(work, "download.pdf")
	makeIngestPDF(t, pdf, "A study of widgets", "")
	now := time.Now()
	if err := os.Chtimes(pdf, now, now); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runIngest(nil) })
	if err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if !strings.Contains(out, "moved "+pdf+" -> smith_2020") {
		t.Fatalf("report: %s", out)
	}
}

func TestIngestRejectsFlagsWithoutFiles(t *testing.T) {
	if err := runIngest([]string{"-into", "smith_2020"}); err == nil {
		t.Fatal("expected usage error")
	}
}
