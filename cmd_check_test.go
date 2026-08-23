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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

func fixtureStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := initStore(t, "")
	return &store.Store{Root: dir}, dir
}

func cleanPaper(key string) *store.Paper {
	return &store.Paper{Key: key, Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963",
			"doi":     "10.1080/01621459.1963.10500830",
		}}}
}

func TestCheckCleanStore(t *testing.T) {
	s, _ := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	if err := runCheck(nil); err != nil {
		t.Errorf("clean store: %v", err)
	}
}

func TestCheckFindsKeyMismatch(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	s.Save(p)
	// dirname/key mismatch: rename the directory
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))
	if err := runCheck(nil); err == nil {
		t.Error("expected error for key/dirname mismatch")
	}
}

func TestCheckFindsSyncConflict(t *testing.T) {
	s, dir := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	os.WriteFile(filepath.Join(dir, "hoeffding_1963",
		"paper.sync-conflict-20260820-XYZ.json"), []byte("{}"), 0o644)
	if err := runCheck(nil); err == nil {
		t.Error("expected error for sync-conflict file")
	}
}

func TestCheckPromotesDraft(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.Status = "draft"
	s.Save(p)
	if err := runCheck(nil); err != nil {
		t.Fatalf("check: %v", err)
	}
	got, _ := s.Load("hoeffding_1963")
	if got.Status != "clean" {
		t.Errorf("status = %q, want clean", got.Status)
	}
}

func TestCheckUnknownKeyArg(t *testing.T) {
	fixtureStore(t)
	if err := runCheck([]string{"nope_1900"}); err == nil {
		t.Error("expected error for unknown key")
	}
}

// TestCheckMismatchReportsUnderDirName covers an entry that is both
// dirname/key mismatched AND has its own CheckPaper error (an invalid
// DOI). Both problems must be printed under the directory's name
// ("wrong_1963"), not under the stale key recorded inside paper.json
// ("hoeffding_1963") - the directory name is the only identity that
// still resolves to a loadable entry.
func TestCheckMismatchReportsUnderDirName(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.DOI = "not-a-doi"
	p.Bibtex.Fields["doi"] = "not-a-doi"
	s.Save(p)
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))

	var runErr error
	out := captureStdout(t, func() { runErr = runCheck(nil) })
	if runErr == nil {
		t.Fatal("expected error for key/dirname mismatch plus DOI problem")
	}

	if strings.Contains(out, "hoeffding_1963:") {
		t.Errorf("output still uses the stale paper.json key, want only the directory name:\n%s", out)
	}
	if !strings.Contains(out, `wrong_1963: error: directory name "wrong_1963" does not match paper.json key "hoeffding_1963"`) {
		t.Errorf("missing dirname/key mismatch line under the directory name:\n%s", out)
	}
	if !strings.Contains(out, "wrong_1963: error: doi:") {
		t.Errorf("missing CheckPaper DOI error line under the directory name:\n%s", out)
	}
}

// TestCheckExplicitKeyFindsMismatch covers "paper check <dir>" (explicit-key
// mode, not whole-store fsck) on an entry whose paper.json key differs from
// its directory name. Previously the dirname/key mismatch check ran only in
// fsck, so this case slipped through: the entry was reported clean, its
// draft status would be promoted, and s.Save(p) would then write to
// s.Dir(p.Key) - creating a NEW directory under the stale body key while the
// old directory kept the draft file behind. The check must fire, under the
// directory name, in explicit-key mode too, and the entry must not be
// promoted or duplicated into a second directory.
func TestCheckExplicitKeyFindsMismatch(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.Status = "draft"
	s.Save(p)
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))

	var runErr error
	out := captureStdout(t, func() { runErr = runCheck([]string{"wrong_1963"}) })
	if runErr == nil {
		t.Fatal("expected error for key/dirname mismatch in explicit-key mode")
	}
	if !strings.Contains(out, `wrong_1963: error: directory name "wrong_1963" does not match paper.json key "hoeffding_1963"`) {
		t.Errorf("missing dirname/key mismatch line under the directory name:\n%s", out)
	}
	if strings.Contains(out, "promoted") {
		t.Errorf("mismatched entry must not be promoted:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "hoeffding_1963")); err == nil {
		t.Error("a new directory named after the stale body key must not be created")
	}
	if _, err := os.Stat(filepath.Join(dir, "wrong_1963", "paper.json")); err != nil {
		t.Errorf("original directory's paper.json should still be there: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "wrong_1963", "paper.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"status": "draft"`) {
		t.Errorf("draft must not have been promoted to clean:\n%s", got)
	}
}

// TestCheckCorruptEntryReportedOnce covers a corrupt paper.json during a
// whole-store run: runCheck loads every entry once, and both the
// per-entry CheckPaper pass and fsck's store-wide pass need that same
// load result, so the "cannot load entry" problem must appear exactly
// once, not once per pass.
func TestCheckCorruptEntryReportedOnce(t *testing.T) {
	_, dir := fixtureStore(t)
	entryDir := filepath.Join(dir, "broken_1900")
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatalf("creating entry directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "paper.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("writing corrupt paper.json: %v", err)
	}

	var runErr error
	out := captureStdout(t, func() { runErr = runCheck(nil) })
	if runErr == nil {
		t.Fatal("expected error for corrupt paper.json")
	}

	count := strings.Count(out, "cannot load entry")
	if count != 1 {
		t.Errorf(`"cannot load entry" appeared %d times, want exactly 1:%s%s`, count, "\n", out)
	}
}

func TestCheckOnline(t *testing.T) {
	dir := initStore(t, "")
	s, _ := store.Open(dir)
	good := &store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "JASA", "year": "1963", "pages": "13--30",
			"doi": "10.1080/01621459.1963.10500830"}}}
	bad := &store.Paper{Key: "phantom_2020", Status: "clean", Holdings: "none",
		DOI: "10.9999/does.not.exist",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Phantom, Paul", "title": "Ghost paper", "journal": "Nowhere",
			"year": "2020", "doi": "10.9999/does.not.exist"}}}
	s.Save(good)
	s.Save(bad)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "10.1080") {
			io.WriteString(w, crossrefWorkResponse)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	overrideBases(t, srv.URL, "", "", "", "")

	out := captureStdout(t, func() {
		err := runCheck([]string{"-online"})
		if err == nil {
			t.Error("unresolvable DOI must make check fail")
		}
	})
	if !strings.Contains(out, "phantom_2020") || !strings.Contains(out, "does not resolve") {
		t.Errorf("output should flag the phantom DOI:\n%s", out)
	}
	if strings.Contains(out, "hoeffding_1963: error") {
		t.Errorf("the good entry must not error:\n%s", out)
	}
}

// TestCheckOnlineDoesNotMisreadA5xxBodyAsNotFound is the regression test
// for the review finding that checkOnline used to detect a 404 by
// matching the literal text "not found" in the error message. getJSON's
// non-404 branch embeds up to 200 raw bytes of the response body in that
// same message, so a 5xx error page (a proxy or CDN failure, a
// rate-limit response, ...) whose body happens to contain the words "not
// found" was misclassified as a hallucinated DOI: an ERROR that wrongly
// blocked draft->clean promotion and the exit code, breaking the rule
// that Crossref being down must not fail every entry. checkOnline must
// instead use errors.Is against the typed sources.ErrNotFound sentinel,
// which only a genuine 404 wraps.
func TestCheckOnlineDoesNotMisreadA5xxBodyAsNotFound(t *testing.T) {
	dir := initStore(t, "")
	s, _ := store.Open(dir)
	flaky := &store.Paper{Key: "flaky_2021", Status: "draft", Holdings: "none",
		DOI: "10.5555/flaky.example",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Flake, Fiona", "title": "A paper behind a flaky proxy", "journal": "Nowhere",
			"year": "2021", "doi": "10.5555/flaky.example"}}}
	phantom := &store.Paper{Key: "phantom_2020", Status: "clean", Holdings: "none",
		DOI: "10.9999/does.not.exist",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Phantom, Paul", "title": "Ghost paper", "journal": "Nowhere",
			"year": "2020", "doi": "10.9999/does.not.exist"}}}
	s.Save(flaky)
	s.Save(phantom)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "10.5555") {
			// A 5xx error page whose body happens to contain the literal
			// text "not found" - unlike an actual 404, this must not be
			// classified as a hallucinated/mistyped DOI.
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"upstream route not found in cache"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	overrideBases(t, srv.URL, "", "", "", "")

	out := captureStdout(t, func() {
		if err := runCheck([]string{"-online"}); err == nil {
			t.Error("the genuinely unresolvable phantom DOI must still make check fail")
		}
	})

	if !strings.Contains(out, "flaky_2021: warning") {
		t.Errorf("a 5xx response must be reported as a warning:\n%s", out)
	}
	if strings.Contains(out, "flaky_2021: error") {
		t.Errorf("a 5xx response body containing \"not found\" must never be classified as a not-found error:\n%s", out)
	}
	if !strings.Contains(out, "flaky_2021: promoted from draft to clean") {
		t.Errorf("a warning-only entry must still be promoted from draft to clean:\n%s", out)
	}
	if !strings.Contains(out, "phantom_2020: error") || !strings.Contains(out, "does not resolve") {
		t.Errorf("the genuinely 404 entry must still be reported as an error:\n%s", out)
	}
}
