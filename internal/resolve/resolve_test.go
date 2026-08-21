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

package resolve

import (
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

func hoeffdingWork() *sources.CrossrefWork {
	return &sources.CrossrefWork{
		DOI:            "10.1080/01621459.1963.10500830",
		Type:           "journal-article",
		Titles:         []string{"Probability Inequalities for Sums of Bounded Random Variables"},
		ContainerTitle: []string{"Journal of the American Statistical Association"},
		Authors:        []sources.CrossrefAuthor{{Family: "Hoeffding", Given: "Wassily"}},
		Volume:         "58", Issue: "301", Page: "13-30",
		Published: sources.CrossrefDate{DateParts: [][]int{{1963, 3}}},
	}
}

func TestFromCrossref(t *testing.T) {
	p, err := FromCrossref(hoeffdingWork())
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "hoeffding_1963" {
		t.Errorf("key = %q", p.Key)
	}
	if p.Status != "draft" || p.Holdings != "none" || p.Pending == "" {
		t.Errorf("status/holdings/pending = %q/%q/%q", p.Status, p.Holdings, p.Pending)
	}
	f := p.Bibtex.Fields
	want := map[string]string{
		"author":  "Hoeffding, Wassily",
		"title":   "Probability Inequalities for Sums of Bounded Random Variables",
		"journal": "Journal of the American Statistical Association",
		"volume":  "58", "number": "301", "pages": "13--30", "year": "1963",
		"doi": "10.1080/01621459.1963.10500830",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %s = %q, want %q", k, f[k], v)
		}
	}
	if p.Bibtex.Type != "article" || p.DOI != want["doi"] {
		t.Errorf("type/doi = %q/%q", p.Bibtex.Type, p.DOI)
	}
}

func TestFromCrossrefEncodesAuthors(t *testing.T) {
	w := hoeffdingWork()
	w.Authors = []sources.CrossrefAuthor{{Family: "Voß", Given: "Jochen"}}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Bibtex.Fields["author"]; got != `Vo{\ss}, Jochen` {
		t.Errorf("author = %q", got)
	}
	if p.Key != "voss_1963" {
		t.Errorf("key = %q, want voss_1963", p.Key)
	}
}

// TestFromCrossrefEncodesJournal pins that the journal field goes through
// tex.Encode like every other free-text field: a journal name containing
// a TeX special character must arrive bibtex-escaped, not raw.
func TestFromCrossrefEncodesJournal(t *testing.T) {
	w := hoeffdingWork()
	w.ContainerTitle = []string{"Statistics & Probability Letters"}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Bibtex.Fields["journal"]; got != `Statistics \& Probability Letters` {
		t.Errorf("journal = %q", got)
	}
}

// TestFromCrossrefOrgAuthor pins the fix for the pile-ingest bug found on
// the LIGO gravitational-wave discovery paper
// (10.1103/PhysRevLett.116.061102): Crossref author arrays can contain a
// collective-author entry that carries only a literal "name", with no
// family/given fields. Such an entry must render as a bibtex literal name
// ("{...}"), not as an empty name part.
func TestFromCrossrefOrgAuthor(t *testing.T) {
	w := hoeffdingWork()
	w.Authors = []sources.CrossrefAuthor{
		{Family: "Abbott", Given: "B. P."},
		{Name: "LIGO Scientific Collaboration"},
	}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	want := `Abbott, B. P. and {LIGO Scientific Collaboration}`
	if got := p.Bibtex.Fields["author"]; got != want {
		t.Errorf("author = %q, want %q", got, want)
	}

	// End-to-end sanity: the braced literal name must parse as a single,
	// non-empty name under Tame-the-BeaST rules, and store.CheckPaper must
	// not flag the author field.
	names, err := bibtex.ParseNames(p.Bibtex.Fields["author"])
	if err != nil {
		t.Fatalf("ParseNames(%q): %v", p.Bibtex.Fields["author"], err)
	}
	if len(names) != 2 {
		t.Fatalf("ParseNames returned %d names, want 2: %+v", len(names), names)
	}
	if names[1].Last != "{LIGO Scientific Collaboration}" || names[1].First != "" {
		t.Errorf("names[1] = %+v", names[1])
	}
	for _, prob := range store.CheckPaper(p) {
		if prob.Severity == "error" {
			t.Errorf("CheckPaper reported an error: %s: %s", prob.Severity, prob.Msg)
		}
	}
}

// TestFromCrossrefSkipsEmptyAuthor pins that an author entry with all of
// Name/Family/Given empty is skipped rather than rejected, as long as at
// least one other entry has real content.
func TestFromCrossrefSkipsEmptyAuthor(t *testing.T) {
	w := hoeffdingWork()
	w.Authors = []sources.CrossrefAuthor{
		{Family: "Abbott", Given: "B. P."},
		{},
		{Name: "LIGO Scientific Collaboration"},
	}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	want := `Abbott, B. P. and {LIGO Scientific Collaboration}`
	if got := p.Bibtex.Fields["author"]; got != want {
		t.Errorf("author = %q, want %q", got, want)
	}
}

// TestFromCrossrefAllAuthorsEmpty pins that the existing "no authors"
// error still applies when every author entry is empty.
func TestFromCrossrefAllAuthorsEmpty(t *testing.T) {
	w := hoeffdingWork()
	w.Authors = []sources.CrossrefAuthor{{}, {}}
	_, err := FromCrossref(w)
	if err == nil {
		t.Fatal("want an error when every author entry is empty")
	}
}

func arxivEntry() *sources.ArxivEntry {
	return &sources.ArxivEntry{
		ID: "2412.05039", Version: 2,
		Title:        "A study of SPDEs in Greenland",
		Authors:      []string{"Jochen Voß", "Andrew M. Stuart"},
		Abstract:     "We study stochastic partial differential equations.",
		Year:         2024,
		PrimaryClass: "math.PR",
	}
}

func TestFromArxiv(t *testing.T) {
	p, err := FromArxiv(arxivEntry())
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "voss_2024" {
		t.Errorf("key = %q", p.Key)
	}
	f := p.Bibtex.Fields
	if f["author"] != `Vo{\ss}, Jochen and Stuart, Andrew M.` {
		t.Errorf("author = %q", f["author"])
	}
	if f["title"] != "A study of {SPDEs} in Greenland" {
		t.Errorf("title = %q", f["title"])
	}
	// The eprint field carries the bare ID: the version lives in
	// paper.json's arxiv.version and in the version-qualified file names.
	if f["eprint"] != "2412.05039" || f["archiveprefix"] != "arXiv" || f["primaryclass"] != "math.PR" {
		t.Errorf("eprint fields = %q/%q/%q", f["eprint"], f["archiveprefix"], f["primaryclass"])
	}
	if p.Bibtex.Type != "misc" || p.Arxiv == nil || p.Arxiv.ID != "2412.05039" || p.Arxiv.Version != 2 {
		t.Errorf("type/arxiv = %q/%+v", p.Bibtex.Type, p.Arxiv)
	}
	if p.Abstract == "" {
		t.Error("abstract should be carried over")
	}
}

func TestMerge(t *testing.T) {
	pub, err := FromCrossref(hoeffdingWork())
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(pub, arxivEntry())
	if m.Bibtex.Type != "article" {
		t.Errorf("merge must keep the published entry type, got %q", m.Bibtex.Type)
	}
	if m.Bibtex.Fields["journal"] == "" {
		t.Error("merge must keep the published journal field")
	}
	if m.Bibtex.Fields["eprint"] != "2412.05039" {
		t.Errorf("eprint = %q", m.Bibtex.Fields["eprint"])
	}
	if m.Arxiv == nil || m.Abstract == "" {
		t.Error("merge must carry the arXiv ref and abstract")
	}
}
