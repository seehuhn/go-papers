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

// crossrefBookWork returns a book-type Crossref work carrying an ISBN
// list, the way Crossref reports it: hyphenated ISBN-10 alongside its
// ISBN-13 equivalent.
func crossrefBookWork() *sources.CrossrefWork {
	return &sources.CrossrefWork{
		DOI:       "10.1000/book-doi",
		Type:      "book",
		Titles:    []string{"A Book About Widgets"},
		Authors:   []sources.CrossrefAuthor{{Family: "Author", Given: "Ann"}},
		ISBN:      []string{"0-521-00601-5", "9780521006019"},
		Published: sources.CrossrefDate{DateParts: [][]int{{1994}}},
	}
}

// TestFromCrossrefBookISBN pins that a book-type work's first valid ISBN
// is normalised onto both the top-level field and bibtex.fields.isbn.
func TestFromCrossrefBookISBN(t *testing.T) {
	p, err := FromCrossref(crossrefBookWork())
	if err != nil {
		t.Fatal(err)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want normalised 9780521006019", p.ISBN)
	}
	if p.Bibtex.Fields["isbn"] != "9780521006019" {
		t.Errorf("bibtex.fields.isbn = %q, want normalised 9780521006019", p.Bibtex.Fields["isbn"])
	}
	if p.Bibtex.Type != "book" {
		t.Errorf("type = %q, want book", p.Bibtex.Type)
	}
}

// TestFromCrossrefBookSkipsInvalidISBN pins that an unparseable ISBN
// entry is skipped in favour of the next one that validates.
func TestFromCrossrefBookSkipsInvalidISBN(t *testing.T) {
	w := crossrefBookWork()
	w.ISBN = []string{"not-an-isbn", "9780521006019"}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want the first valid entry, normalised", p.ISBN)
	}
}

// TestFromCrossrefNonBookLeavesBibtexISBNUnset pins that a non-book work
// with an ISBN in the Crossref record (e.g. a proceedings article citing
// its volume's ISBN) still records the top-level ISBN, but does not set
// bibtex.fields.isbn: that promotion is reserved for book-type works.
func TestFromCrossrefNonBookLeavesBibtexISBNUnset(t *testing.T) {
	w := hoeffdingWork()
	w.ISBN = []string{"9780521006019"}
	p, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	if p.ISBN != "9780521006019" {
		t.Errorf("ISBN = %q, want 9780521006019", p.ISBN)
	}
	if _, ok := p.Bibtex.Fields["isbn"]; ok {
		t.Errorf("bibtex.fields.isbn = %q, want unset for a non-book work", p.Bibtex.Fields["isbn"])
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

// TestFromCrossrefRejectsContainerType pins the fix for the PNAS ingest
// bug: a Crossref record whose type names a container - the journal
// itself, here - rather than a single work must be rejected with a
// message that says what happened, not "missing authors", even though a
// container record does also have zero authors.
func TestFromCrossrefRejectsContainerType(t *testing.T) {
	w := &sources.CrossrefWork{
		DOI:    "10.1073/pnas",
		Type:   "journal",
		Titles: []string{"Proceedings of the National Academy of Sciences"},
	}
	_, err := FromCrossref(w)
	if err == nil {
		t.Fatal("want an error for a container-type Crossref record")
	}
	want := "10.1073/pnas is a journal DOI, not an article"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
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

// TestMergeKeepsPublishedDOIOnConflict pins that the published record's
// DOI is authoritative: even when the arXiv entry names a different DOI,
// the merge keeps published.DOI untouched (RecordIdentifiers refuses to
// write on a genuine conflict, so the published value survives).
func TestMergeKeepsPublishedDOIOnConflict(t *testing.T) {
	pub, err := FromCrossref(hoeffdingWork())
	if err != nil {
		t.Fatal(err)
	}
	e := arxivEntry()
	e.DOI = "10.9999/does-not-match"
	m := Merge(pub, e)
	if m.DOI != pub.DOI {
		t.Errorf("DOI = %q, want the published DOI %q kept", m.DOI, pub.DOI)
	}
}

// TestMergeFillsMissingDOIFromArxiv pins that when the published record
// itself carries no DOI, the arXiv entry's DOI (the one that led to the
// published record in the first place) is recorded.
func TestMergeFillsMissingDOIFromArxiv(t *testing.T) {
	w := hoeffdingWork()
	w.DOI = ""
	pub, err := FromCrossref(w)
	if err != nil {
		t.Fatal(err)
	}
	e := arxivEntry()
	e.DOI = "10.1080/01621459.1963.10500830"
	m := Merge(pub, e)
	if m.DOI != e.DOI {
		t.Errorf("DOI = %q, want %q filled in from the arXiv entry", m.DOI, e.DOI)
	}
}

// TestMergeKeepsPublishedISBN pins that a book's ISBN survives the
// merge unchanged: an arXiv preprint carries no ISBN of its own to
// contribute or conflict with.
func TestMergeKeepsPublishedISBN(t *testing.T) {
	pub, err := FromCrossref(crossrefBookWork())
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(pub, arxivEntry())
	if m.ISBN != pub.ISBN {
		t.Errorf("ISBN = %q, want the published ISBN %q kept", m.ISBN, pub.ISBN)
	}
}
