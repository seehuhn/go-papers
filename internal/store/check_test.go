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

package store

import (
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
)

// makeCleanPaper returns a well-formed Paper (the hoeffding_1963 article
// entry) that CheckPaper must accept without any problems.
func makeCleanPaper() *Paper {
	return &Paper{
		Key:    "hoeffding_1963",
		Status: "clean",
		Bibtex: bibtex.Entry{
			Type: "article",
			Fields: map[string]string{
				"author":  "Hoeffding, Wassily",
				"title":   "Probability inequalities for sums of bounded random variables",
				"journal": "Journal of the American Statistical Association",
				"volume":  "58",
				"number":  "301",
				"pages":   "13--30",
				"year":    "1963",
				"doi":     "10.1080/01621459.1963.10500830",
			},
		},
		DOI:      "10.1080/01621459.1963.10500830",
		Holdings: "none",
	}
}

func problemsContain(ps []Problem, substr string) bool {
	for _, p := range ps {
		if strings.Contains(p.Msg, substr) {
			return true
		}
	}
	return false
}

func TestCheckClean(t *testing.T) {
	if ps := CheckPaper(makeCleanPaper()); len(ps) != 0 {
		t.Errorf("clean paper has problems: %v", ps)
	}
}

// Rule 1
func TestCheckUnknownType(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Type = "blogpost"
	if !problemsContain(CheckPaper(p), "unknown entry type") {
		t.Error("missing unknown-type problem")
	}
}

// Rule 2
func TestCheckMissingRequired(t *testing.T) {
	p := makeCleanPaper()
	delete(p.Bibtex.Fields, "journal")
	if !problemsContain(CheckPaper(p), "journal") {
		t.Error("missing required-field problem")
	}
}

// Rule 2 (any-of group): a book entry with editor but no author must not
// report a missing author/editor problem; the same entry with neither
// must report one.
func TestCheckRequiredFieldGroupSatisfiedByEditor(t *testing.T) {
	p := &Paper{
		Key:    "essays_2020",
		Status: "clean",
		Bibtex: bibtex.Entry{
			Type: "book",
			Fields: map[string]string{
				"editor":    "Smith, Jane",
				"title":     "A collection of essays",
				"publisher": "Acme Press",
				"year":      "2020",
			},
		},
		Holdings: "none",
	}
	if ps := CheckPaper(p); len(ps) != 0 {
		t.Errorf("book with editor (no author) has problems: %v", ps)
	}
}

func TestCheckRequiredFieldGroupMissingBoth(t *testing.T) {
	p := &Paper{
		Key:    "essays_2020",
		Status: "clean",
		Bibtex: bibtex.Entry{
			Type: "book",
			Fields: map[string]string{
				"title":     "A collection of essays",
				"publisher": "Acme Press",
				"year":      "2020",
			},
		},
		Holdings: "none",
	}
	if !problemsContain(CheckPaper(p), "author or editor") {
		t.Error("missing required-field problem for author/editor group")
	}
}

// Rule 3
func TestCheckUnparsableAuthor(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["author"] = "A, B, C, D"
	if !problemsContain(CheckPaper(p), "author") {
		t.Error("missing unparsable-author problem")
	}
}

// Rule 4
func TestCheckUnknownMacro(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["title"] = `On \frobnicate{things}`
	if !problemsContain(CheckPaper(p), "frobnicate") {
		t.Error("missing unknown-macro problem")
	}
}

// Rule 5
func TestCheckUnbalancedBraces(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["title"] = "A study of {unbalanced braces"
	if !problemsContain(CheckPaper(p), "unbalanced braces") {
		t.Error("missing unbalanced-braces problem")
	}
}

// Rule 6
func TestCheckInvalidDOI(t *testing.T) {
	p := makeCleanPaper()
	p.DOI = "not-a-doi"
	p.Bibtex.Fields["doi"] = "not-a-doi"
	if !problemsContain(CheckPaper(p), "not-a-doi") {
		t.Error("missing invalid-DOI problem")
	}
}

// Rule 7
func TestCheckInvalidArxiv(t *testing.T) {
	p := makeCleanPaper()
	p.Arxiv = &ArxivRef{ID: "not-an-arxiv-id"}
	if !problemsContain(CheckPaper(p), "not-an-arxiv-id") {
		t.Error("missing invalid-arxiv problem")
	}
}

// Rule 8
func TestCheckInvalidStatus(t *testing.T) {
	p := makeCleanPaper()
	p.Status = "archived"
	if !problemsContain(CheckPaper(p), "archived") {
		t.Error("missing invalid-status problem")
	}
}

func TestCheckInvalidHoldings(t *testing.T) {
	p := makeCleanPaper()
	p.Holdings = "lost"
	if !problemsContain(CheckPaper(p), "lost") {
		t.Error("missing invalid-holdings problem")
	}
}

// Rule 9
func TestCheckSinglePageDash(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["pages"] = "13-30"
	if !problemsContain(CheckPaper(p), "--") {
		t.Error("missing page-range warning")
	}
}

// Rule 10
func TestCheckCapitalizedSuspect(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["title"] = "A study of SPDEs in Greenland"
	ps := CheckPaper(p)
	if !problemsContain(ps, "Greenland") || !problemsContain(ps, "SPDEs") {
		t.Errorf("missing capitalization warnings: %v", ps)
	}
}

func TestCheckBraceProtectedOK(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["title"] = "A study of {SPDEs} in {G}reenland"
	if ps := CheckPaper(p); len(ps) != 0 {
		t.Errorf("brace-protected title has problems: %v", ps)
	}
}

// Rule 11
func TestCheckArticleMissingDOI(t *testing.T) {
	p := makeCleanPaper()
	delete(p.Bibtex.Fields, "doi")
	p.DOI = ""
	if !problemsContain(CheckPaper(p), "doi") {
		t.Error("missing missing-DOI warning")
	}
}

// Rule 12
func TestCheckDraftStatus(t *testing.T) {
	p := makeCleanPaper()
	p.Status = "draft"
	if !problemsContain(CheckPaper(p), "draft") {
		t.Error("missing draft-status notice")
	}
}

// Rule 13
func TestCheckDOIInconsistent(t *testing.T) {
	p := makeCleanPaper()
	p.DOI = "10.1000/other-doi"
	if !problemsContain(CheckPaper(p), "10.1000/other-doi") {
		t.Error("missing DOI-inconsistency problem")
	}
}

func TestCheckEprintInconsistent(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["eprint"] = "1234.5678"
	p.Arxiv = &ArxivRef{ID: "8765.4321"}
	if !problemsContain(CheckPaper(p), "8765.4321") {
		t.Error("missing eprint-inconsistency problem")
	}
}

func TestCheckRawSpecialChar(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["journal"] = "Statistics & Probability Letters"
	ps := CheckPaper(p)
	if !problemsContain(ps, `bibtex.fields.journal: raw "&"`) {
		t.Errorf("missing raw-ampersand problem, got %v", ps)
	}
	for _, prob := range ps {
		if strings.Contains(prob.Msg, `raw "&"`) && prob.Severity != "warning" {
			t.Errorf("severity = %q, want warning", prob.Severity)
		}
	}
}

func TestCheckEscapedSpecialCharOK(t *testing.T) {
	p := makeCleanPaper()
	p.Bibtex.Fields["journal"] = `Statistics \& Probability Letters`
	p.Bibtex.Fields["note"] = `$100\%$ and $a & b$ in math mode`
	if ps := CheckPaper(p); problemsContain(ps, "raw") {
		t.Errorf("escaped and math-mode specials must pass, got %v", ps)
	}
}
