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
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

// TestQualityProblemsAcceptsManual pins Finding 3: standard BibTeX entry
// types (here, manual) must not be rejected as unknown.
func TestQualityProblemsAcceptsManual(t *testing.T) {
	e := bibtex.KeyedEntry{Key: "m", Entry: bibtex.Entry{Type: "manual",
		Fields: map[string]string{"title": "Some Manual"}}}

	got := qualityProblems(e)

	for _, p := range got {
		if strings.Contains(p, "unknown entry type") {
			t.Errorf("manual is a standard bibtex type, must not be unknown: %v", got)
		}
	}
}

func TestAuditConfirmsAResolvableDOI(t *testing.T) {
	dir := initStore(t, "test@example.org")
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	refuse := refusingServer(t)
	overrideBases(t, crossrefSrv.URL, refuse, refuse, refuse, refuse)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{hoef,
  author = {Hoeffding, Wassily},
  title = {Probability inequalities for sums of bounded random variables},
  journal = {J. Amer. Statist. Assoc.},
  year = {1963},
  doi = {10.1080/01621459.1963.10500830},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "confirmed" {
		t.Errorf("Existence = %q, want confirmed", r.Entries[0].Existence)
	}
}

// refusingServer returns the URL of a server that fails the test if it is
// contacted. overrideBases leaves an empty base pointing at the real
// production API, so every service a test does not expect must be given
// one of these.
func refusingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAuditReportsNotFoundWhenNothingMatches(t *testing.T) {
	dir := initStore(t, "test@example.org")
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[]}}`)
	}))
	t.Cleanup(empty.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, empty.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{ghost,
  author = {Nobody, A.},
  title = {A paper that does not exist anywhere},
  journal = {J. Nothing},
  year = {2019},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	if !strings.Contains(out, "likely hallucinated") {
		t.Errorf("a reference no source knows must be flagged:\n%s", out)
	}
}

func TestAuditReportsUnverifiedWithCandidates(t *testing.T) {
	dir := initStore(t, "test@example.org")
	// One near-miss candidate: same author, title close but not close enough.
	near := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[{
			"DOI":"10.1000/near",
			"title":["Multilevel quasi-Monte Carlo path simulation"],
			"author":[{"family":"Giles","given":"Michael"}],
			"issued":{"date-parts":[[2009]]}}]}}`)
	}))
	t.Cleanup(near.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, near.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{giles,
  author = {Giles, Michael B.},
  title = {Multilevel Monte Carlo path simulation},
  journal = {Oper. Res.},
  year = {2008},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	if strings.Contains(out, "likely hallucinated") {
		t.Errorf("a near candidate is not nothing; must not be called hallucinated:\n%s", out)
	}
	if !strings.Contains(out, "quasi") {
		t.Errorf("the near candidate should be listed for the agent to judge:\n%s", out)
	}
}

func TestAuditOnlineRechecksStoreEntries(t *testing.T) {
	dir := initStore(t, "test@example.org")
	s := openConfiguredStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "JASA", "year": "1963"}}})

	var hits int
	crossrefSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		io.WriteString(w, crossrefWorkResponse)
	}))
	t.Cleanup(crossrefSrv.Close)
	refuse := refusingServer(t)
	overrideBases(t, crossrefSrv.URL, refuse, refuse, refuse, refuse)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte("@article{h,\n  doi = {10.1080/01621459.1963.10500830},\n"+
		"  author = {Hoeffding, Wassily},\n  title = {Probability inequalities},\n"+
		"  journal = {JASA},\n  year = {1963},\n}"), 0o644)

	captureStdout(t, func() { runAudit([]string{bib}) })
	if hits != 0 {
		t.Errorf("a store-held clean entry must not be re-resolved without -online; %d calls", hits)
	}
	captureStdout(t, func() { runAudit([]string{"-online", bib}) })
	if hits == 0 {
		t.Error("-online must re-resolve store-held entries")
	}
}

// TestAuditOnlineNeverDemotesAStoreConfirmedEntry pins the controller
// ruling on Finding 1: the store is itself a source, so an online
// disagreement can add doubt as a Problems note but must never outvote a
// paper that is physically held. Existence must stay "confirmed" even
// when the source that would have re-verified it is down.
func TestAuditOnlineNeverDemotesAStoreConfirmedEntry(t *testing.T) {
	dir := initStore(t, "test@example.org")
	s := openConfiguredStore(t)
	if err := s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "JASA", "year": "1963"}}}); err != nil {
		t.Fatal(err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "crossref is down")
	}))
	t.Cleanup(down.Close)
	refuse := refusingServer(t)
	overrideBases(t, down.URL, refuse, refuse, refuse, refuse)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte("@article{h,\n  doi = {10.1080/01621459.1963.10500830},\n"+
		"  author = {Hoeffding, Wassily},\n  title = {Probability inequalities},\n"+
		"  journal = {JASA},\n  year = {1963},\n}"), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-online", "-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "confirmed" {
		t.Errorf("Existence = %q, want confirmed (a store-held entry must never be demoted)", r.Entries[0].Existence)
	}
	found := false
	for _, p := range r.Entries[0].Problems {
		if strings.Contains(p, "online re-verification: unchecked") {
			found = true
		}
	}
	if !found {
		t.Errorf("Problems should record the failed re-verification: %v", r.Entries[0].Problems)
	}
}

// TestAuditConfirmsWhenTitleClearsBarAndCorroborates and
// TestAuditUnverifiedWhenTitleClearsBarButDoesNotCorroborate pin Finding
// 2: corroboratesCandidate must actually run and gate the confirm
// decision, not just the titleBar check. Both use a candidate whose title
// exactly reproduces the bib title (similarity 1.0, safely above
// titleBar), so only corroboration differs between them.
func TestAuditConfirmsWhenTitleClearsBarAndCorroborates(t *testing.T) {
	dir := initStore(t, "test@example.org")
	hit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[{
			"DOI":"10.1000/exact",
			"title":["Robust estimation of a location parameter"],
			"author":[{"family":"Huber","given":"Peter J."}],
			"issued":{"date-parts":[[1964]]}}]}}`)
	}))
	t.Cleanup(hit.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, hit.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{huber,
  author = {Huber, Peter J.},
  title = {Robust estimation of a location parameter},
  journal = {Ann. Math. Statist.},
  year = {1964},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "confirmed" {
		t.Errorf("Existence = %q, want confirmed (title matches and the author corroborates)", r.Entries[0].Existence)
	}
}

func TestAuditUnverifiedWhenTitleClearsBarButDoesNotCorroborate(t *testing.T) {
	dir := initStore(t, "test@example.org")
	hit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[{
			"DOI":"10.1000/exact2",
			"title":["Optimal transport methods in economics"],
			"author":[{"family":"Nguyen","given":"Anh"}],
			"issued":{"date-parts":[[2020]]}}]}}`)
	}))
	t.Cleanup(hit.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, hit.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{smith,
  author = {Smith, John},
  title = {Optimal transport methods in economics},
  journal = {J. Econ. Theory},
  year = {1980},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "unverified" {
		t.Errorf("Existence = %q, want unverified (title matches but neither author nor year corroborates)", r.Entries[0].Existence)
	}
}

// TestAuditDOILookupFailureIsUnchecked and TestAuditAllSearchSourcesDownIsUnchecked
// pin Finding 3's two controller rulings that a down source must never
// read as "notFound".
func TestAuditDOILookupFailureIsUnchecked(t *testing.T) {
	dir := initStore(t, "test@example.org")
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "crossref is down")
	}))
	t.Cleanup(down.Close)
	refuse := refusingServer(t)
	overrideBases(t, down.URL, refuse, refuse, refuse, refuse)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{d,
  author = {Someone, A.},
  title = {A paper with a doi crossref cannot reach},
  journal = {J. Whatever},
  year = {2020},
  doi = {10.1000/whatever},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "unchecked" {
		t.Errorf("Existence = %q, want unchecked (crossref being down is not proof the paper does not exist)", r.Entries[0].Existence)
	}
}

func TestAuditAllSearchSourcesDownIsUnchecked(t *testing.T) {
	dir := initStore(t, "test@example.org")
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "down")
	}))
	t.Cleanup(down.Close)
	refuse := refusingServer(t)
	overrideBases(t, down.URL, refuse, refuse, down.URL, down.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@article{e,
  author = {Someone, B.},
  title = {A paper no search source can currently be asked about},
  journal = {J. Whatever},
  year = {2020},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "unchecked" {
		t.Errorf("Existence = %q, want unchecked (every search source being down proves nothing)", r.Entries[0].Existence)
	}
}

// TestAuditConfirmsViaArxivID exercises the arXiv identifier branch of
// verify, which previously had no direct coverage.
func TestAuditConfirmsViaArxivID(t *testing.T) {
	dir := initStore(t, "test@example.org")
	arxivSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, arxivResponseNoDOI)
	}))
	t.Cleanup(arxivSrv.Close)
	refuse := refusingServer(t)
	overrideBases(t, refuse, arxivSrv.URL, refuse, refuse, refuse)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(`@misc{voss,
  author = {Vo{\ss}, Jochen},
  title = {A study of SPDEs in Greenland},
  year = {2024},
  eprint = {2412.05039},
}`), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{"-json", bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("parsing the report: %v", err)
	}
	if r.Entries[0].Existence != "confirmed" {
		t.Errorf("Existence = %q, want confirmed (the arXiv ID resolved)", r.Entries[0].Existence)
	}
}

func TestAuditOfflineMatchesTheStore(t *testing.T) {
	dir := initStore(t, "test@example.org")
	guardBases(t)
	s := openConfiguredStore(t)
	p := &store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "J. Amer. Statist. Assoc.", "year": "1963"}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	bib := filepath.Join(dir, "refs.bib")
	if err := os.WriteFile(bib, []byte(`@article{hoef,
  author = {Hoeffding, Wassily},
  title = {Probability inequalities},
  year = {1963},
  doi = {10.1080/01621459.1963.10500830},
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runAudit([]string{bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	if !strings.Contains(out, "hoeffding_1963") {
		t.Errorf("output should name the matched store entry:\n%s", out)
	}
	if !strings.Contains(out, "journal") {
		t.Errorf("the bib entry is missing the journal the store has; audit should say so:\n%s", out)
	}
}

func TestAuditNeverWritesToTheStore(t *testing.T) {
	dir := initStore(t, "")
	// This entry has no doi or store match, so verify legitimately searches
	// the sources — that is the point of Task 3. What must not happen is a
	// write to the store, which is all this test checks; the sources are
	// given empty responses rather than guardBases's refusing ones so the
	// search completes normally instead of failing on contact.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[]}}`)
	}))
	t.Cleanup(empty.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, empty.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)
	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte("@article{k,\n  title = {Nothing},\n  year = {1999},\n}"), 0o644)
	before := storeSnapshot(t, dir)

	captureStdout(t, func() { runAudit([]string{bib}) })

	if after := storeSnapshot(t, dir); after != before {
		t.Errorf("audit is read-only but the store changed:\nbefore %v\nafter  %v", before, after)
	}
}

// TestAuditStoreHeldDraftIsNeverCalledHallucinated pins Finding 1: a bib
// entry that matches a store entry the store actually holds must never be
// reported as "likely hallucinated", even when the store entry is a draft
// (so it falls through to online verification) and every online source
// comes back empty. The accusation is false on its face — the store holds
// the paper — so the clamp must turn the verdict into "unverified" and
// name the store key as counter-evidence instead.
func TestAuditStoreHeldDraftIsNeverCalledHallucinated(t *testing.T) {
	dir := initStore(t, "test@example.org")
	s := openConfiguredStore(t)
	p := &store.Paper{Key: "giles_2008", Status: "draft", Holdings: "published",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Giles, Michael B.", "title": "Multilevel Monte Carlo path simulation",
			"journal": "Oper. Res.", "year": "2008"}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"message":{"items":[]}}`)
	}))
	t.Cleanup(empty.Close)
	emptyList := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(emptyList.Close)
	overrideBases(t, empty.URL, refusingServer(t), refusingServer(t), emptyList.URL, emptyList.URL)

	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(bibForPaper(p)), 0o644)

	out := captureStdout(t, func() {
		if err := runAudit([]string{bib}); err != nil {
			t.Fatalf("audit: %v", err)
		}
	})

	if strings.Contains(out, "likely hallucinated") {
		t.Errorf("a store-held entry must never be called likely hallucinated:\n%s", out)
	}
	if !strings.Contains(out, "giles_2008") {
		t.Errorf("output should name the store key as counter-evidence:\n%s", out)
	}
}

// cleanPaperForAudit returns a valid clean article entry holding
// published.pdf, for tests that need a store match without caring about
// the bibliographic details.
func cleanPaperForAudit() *store.Paper {
	return &store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author": "Hoeffding, Wassily", "title": "Probability inequalities",
			"journal": "J. Amer. Statist. Assoc.", "year": "1963",
			"doi": "10.1080/01621459.1963.10500830"}},
		Versions: map[string]store.Version{"published.pdf": {Acquired: "2026-08-20"}}}
}

// bibForPaper renders p's bibtex entry as a .bib file body, so a test can
// audit exactly the reference the store already holds.
func bibForPaper(p *store.Paper) string {
	return bibtex.Format(p.Key, p.Bibtex)
}

func TestAuditReportsRecordedClaims(t *testing.T) {
	dir := initStore(t, "")
	guardBases(t)
	s := openConfiguredStore(t)
	p := cleanPaperForAudit()
	p.Audit = &store.Audit{Claims: []store.Claim{{
		Claim: "bounds the deviation", Verdict: "supports",
		Version: "published.pdf", Date: "2026-08-23"}}}
	s.Save(p)
	bib := filepath.Join(dir, "refs.bib")
	os.WriteFile(bib, []byte(bibForPaper(p)), 0o644)

	out := captureStdout(t, func() { runAudit([]string{bib}) })

	if !strings.Contains(out, "1 claim") {
		t.Errorf("output should say a claim is already recorded:\n%s", out)
	}
}

// storeSnapshot returns a stable description of every file under dir, so a
// test can assert that a command left the store untouched.
func storeSnapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %d\n", path, info.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
