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

func TestIngestSinceAndInto(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	s, _ := store.Open("")
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "JASA", "year": "1963"}}})
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "old.pdf")
	newFile := filepath.Join(dir, "new.pdf")
	makeIngestPDF(t, oldFile, "Something Unrelated", "10.9999/other")
	makeIngestPDF(t, newFile, "Probability inequalities", "10.1080/01621459.1963.10500830")
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, past, past); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-time.Hour).Format(time.RFC3339)

	err := runIngest([]string{"-since", cutoff, "-into", "hoeffding_1963", oldFile, newFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("surviving file should be attached as published.pdf")
	}
	if _, err := os.Stat(newFile); !errors.Is(err, os.ErrNotExist) {
		t.Error("attached file must be moved, not copied")
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Error("filtered-out file must be left in place")
	}
	p, _ := s.Load("hoeffding_1963")
	if p.Holdings != "published" {
		t.Errorf("holdings = %q", p.Holdings)
	}
}

func TestIngestIntoNeedsExactlyOne(t *testing.T) {
	fetchFixtureStore(t)
	s, _ := store.Open("")
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
}

func TestIngestIntoVerifiesIdentity(t *testing.T) {
	fetchFixtureStore(t)
	s, _ := store.Open("")
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
}

func TestIngestBatchCreatesEntries(t *testing.T) {
	storeDir := fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse) // always the Hoeffding record
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "")
	f := filepath.Join(t.TempDir(), "paper.pdf")
	makeIngestPDF(t, f, "Probability inequalities", "10.1080/01621459.1963.10500830")

	if err := runIngest([]string{f}); err != nil {
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
	if _, err := os.Stat(filepath.Join(storeDir, "hoeffding_1963", "published.pdf")); err != nil {
		t.Error("file should have been moved into the store")
	}
	if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
		t.Error("source file must be moved away")
	}
}

func TestIngestDOIOverride(t *testing.T) {
	fetchFixtureStore(t)
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	overrideBases(t, crossrefSrv.URL, "", "", "", "")
	f := filepath.Join(t.TempDir(), "scan.pdf")
	makeIngestPDF(t, f, "", "") // no text, unidentifiable on its own

	err := runIngest([]string{"-doi", "10.1080/01621459.1963.10500830", f})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := store.Open("")
	if _, err := s.Load("hoeffding_1963"); err != nil {
		t.Error("entry should have been created from the override DOI")
	}
}

func TestIngestUnidentifiable(t *testing.T) {
	fetchFixtureStore(t)
	f := filepath.Join(t.TempDir(), "scan.pdf")
	makeIngestPDF(t, f, "", "")

	err := runIngest([]string{f})
	if err == nil || !strings.Contains(err.Error(), "OCR") {
		t.Errorf("text-free PDF must report the scanned case, got %v", err)
	}
	if _, statErr := os.Stat(f); statErr != nil {
		t.Error("unidentified file must be left in place")
	}
	s, _ := store.Open("")
	keys, _ := s.Keys()
	if len(keys) != 0 {
		t.Errorf("no entry may be created, found %v", keys)
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
	overrideBases(t, guardSrv.URL, arxivSrv.URL, "", "", "")
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
	s, _ := store.Open("")
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
