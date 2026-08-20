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
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

// saveSearchFixture saves the same two papers used by
// internal/store/search_test.go's searchFixture, so "paper search" can
// be exercised end to end through runSearch.
func saveSearchFixture(t *testing.T, s *store.Store) {
	t.Helper()
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963"}}})
	s.Save(&store.Paper{Key: "voss_2004", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "phdthesis", Fields: map[string]string{
			"author": `Vo{\ss}, Jochen`,
			"title":  "Some large deviation results for diffusion processes",
			"school": "Universit{\"a}t Kaiserslautern",
			"year":   "2004"}}})
}

func TestSearchJSONDecodesAuthor(t *testing.T) {
	s, _ := fixtureStore(t)
	saveSearchFixture(t, s)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSearch([]string{"-json", "voss"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}

	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshaling -json output: %v\noutput:\n%s", err, out)
	}
	if len(results) != 1 {
		t.Fatalf("results = %v, want 1 hit", results)
	}
	if !strings.Contains(out, `"Voß, Jochen"`) {
		t.Errorf("expected decoded author \"Voß, Jochen\" in output:\n%s", out)
	}
}

func TestSearchNoTermsNoFilterErrors(t *testing.T) {
	fixtureStore(t)
	if err := runSearch(nil); err == nil {
		t.Error("expected error for no terms and no filter")
	}
}

// TestSearchHumanFlagsDraftAndDeprecated covers the human ("paper search",
// no -json) output format: the spec's CLI table and README promise that
// human output flags drafts and deprecated versions the same way the JSON
// "flags" array does. Before this fix, only the "[holdings]" marker was
// printed; "[draft]" and "[deprecated:<file>]" were silently dropped.
func TestSearchHumanFlagsDraftAndDeprecated(t *testing.T) {
	s, _ := fixtureStore(t)
	s.Save(&store.Paper{Key: "hoeffding_1963", Status: "draft", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963"}},
		Versions: map[string]store.Version{
			"hoeffding.pdf": {Deprecated: true},
		},
	})

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSearch([]string{"hoeffding"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}
	if !strings.Contains(out, "[draft]") {
		t.Errorf("expected human output to contain [draft]:\n%s", out)
	}
	if !strings.Contains(out, "[deprecated:hoeffding.pdf]") {
		t.Errorf("expected human output to contain [deprecated:hoeffding.pdf]:\n%s", out)
	}
}

func TestSearchNoTermsWithFilterReturnsAll(t *testing.T) {
	s, _ := fixtureStore(t)
	saveSearchFixture(t, s)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSearch([]string{"-json", "-holdings", "published"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshaling -json output: %v\noutput:\n%s", err, out)
	}
	if len(results) != 1 || results[0]["key"] != "hoeffding_1963" {
		t.Errorf("results = %v, want only hoeffding_1963", results)
	}
}
