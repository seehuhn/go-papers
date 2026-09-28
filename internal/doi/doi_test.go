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

package doi

import (
	"reflect"
	"testing"
)

// sici is the legacy Wiley SICI DOI that motivated this package: the old
// shared regex excluded '<'/'>' from the suffix and truncated it at the
// first '<'. The DOI Handbook makes the suffix opaque, so Syntactic must
// accept it whole.
const sici = `10.1002/(SICI)1097-0258(19960229)15:4<361::AID-SIM168>3.0.CO;2-4`

func TestSyntactic(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{sici, true},
		{"10.1000.10/123456", true}, // sub-divided registrant code
		{"10.1080/01621459.1963.10500830", true},
		{"9.1234/x", false}, // does not start with "10."
		{"10.abc/x", false}, // registrant is not digits
		{"10.1234/", false}, // empty suffix
		{"10.1234/with space", false},
	}
	for _, c := range cases {
		if got := Syntactic(c.in); got != c.want {
			t.Errorf("Syntactic(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCandidatesWholeSICI checks that a SICI DOI immediately followed by
// sentence punctuation is extracted whole: the trailing sentence period
// is stripped, but the internal parentheses and angle brackets - which
// used to be excluded from the DOI suffix entirely - are left alone.
func TestCandidatesWholeSICI(t *testing.T) {
	text := "See " + sici + "."
	got := Candidates(text)
	want := []string{sici + ".", sici}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

func TestCandidatesNoTrailingPunctuation(t *testing.T) {
	text := "the paper is " + sici + " in full"
	got := Candidates(text)
	want := []string{sici}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

func TestCandidatesTrimLadder(t *testing.T) {
	text := "DOI: 10.1214/aop/1176996548."
	got := Candidates(text)
	want := []string{"10.1214/aop/1176996548.", "10.1214/aop/1176996548"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

// TestCandidatesTrimLadderMultipleChars checks that trailing punctuation
// is peeled one character at a time, longest first, until a
// non-punctuation byte is reached - producing three rungs here, the last
// of which is the DOI without any of the prose punctuation that followed
// it in the text.
func TestCandidatesTrimLadderMultipleChars(t *testing.T) {
	text := "cf. 10.1234/abc.,"
	got := Candidates(text)
	want := []string{"10.1234/abc.,", "10.1234/abc.", "10.1234/abc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

// TestCandidatesUnbalancedTrailingParenIsProse checks that a DOI wrapped
// in a prose parenthetical loses the closing ')' that belongs to the
// sentence, not the DOI - because it is unbalanced within the matched
// candidate.
func TestCandidatesUnbalancedTrailingParenIsProse(t *testing.T) {
	text := "(see 10.1234/abc.v2)"
	got := Candidates(text)
	want := []string{"10.1234/abc.v2)", "10.1234/abc.v2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

// TestCandidatesBalancedTrailingParenIsPartOfDOI checks that a DOI whose
// suffix legitimately ends in a balanced ')' keeps it - the SICI case in
// miniature.
func TestCandidatesBalancedTrailingParenIsPartOfDOI(t *testing.T) {
	text := "cf. 10.1234/foo(bar)"
	got := Candidates(text)
	want := []string{"10.1234/foo(bar)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

func TestCandidatesMultipleMatches(t *testing.T) {
	text := "10.1101/aaa, and also 10.2222/bbb."
	got := Candidates(text)
	want := []string{"10.1101/aaa,", "10.1101/aaa", "10.2222/bbb.", "10.2222/bbb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

func TestCandidatesSubdividedRegistrant(t *testing.T) {
	text := "see 10.1000.10/123456 for the record"
	got := Candidates(text)
	want := []string{"10.1000.10/123456"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

// TestCandidatesDropsTruncatedPrefix pins the bug behind "paper ingest"
// filing a PNAS article under its journal's own DOI: page 1 breaks
// "10.1073/pnas.2422633122" across a line inside a supplementary-material
// URL, truncating the extracted run at the line break to
// "10.1073/pnas." - a real, separately registered DOI (the PNAS journal
// itself) - while the article's own, untruncated DOI appears intact
// elsewhere on the same page. The truncated run is a proper prefix of the
// real one and must be dropped, leaving only the real DOI's ladder.
func TestCandidatesDropsTruncatedPrefix(t *testing.T) {
	text := "at https://www.pnas.org/lookup/suppl/doi:10.1073/pnas.\n" +
		"2422633122/-/DCSupplemental.\n" +
		"See https://doi.org/10.1073/pnas.2422633122 for details."
	got := Candidates(text)
	want := []string{"10.1073/pnas.2422633122"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

// TestCandidatesKeepsUnrelatedShortMatch checks that dropping a prefix
// match does not over-fire: a short DOI that is not a textual prefix of
// the longer one - merely sharing the same registrant - is kept.
func TestCandidatesKeepsUnrelatedShortMatch(t *testing.T) {
	text := "10.1073/pnaz and 10.1073/pnas.2422633122"
	got := Candidates(text)
	want := []string{"10.1073/pnaz", "10.1073/pnas.2422633122"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
	}
}

func TestCandidatesNone(t *testing.T) {
	if got := Candidates("no identifiers here"); got != nil {
		t.Errorf("Candidates = %q, want nil", got)
	}
}
