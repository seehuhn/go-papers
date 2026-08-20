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

// TestEndToEndWorkflow exercises the full "paper" workflow through the
// exported run* functions, against a store built from three entries: a
// clean article (with DOI), a draft with a deliberate unknown macro in
// one field, and a clean phdthesis whose author name exercises TeX
// decoding (Vo{\ss} -> Voß). It walks the loop the design spec expects an
// agent to drive: check reports a problem, an edit fixes it, check
// passes and promotes the draft, and search/bib then see the corrected,
// promoted store.
func TestEndToEndWorkflow(t *testing.T) {
	s, _ := fixtureStore(t)

	article := cleanPaper("hoeffding_1963") // clean article, incl. DOI

	draft := &store.Paper{
		Key: "smith_2020", Status: "draft", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "misc", Fields: map[string]string{
			"author": "Smith, John",
			"title":  `A study of random fields, with a \badmacro in it`,
			"year":   "2020",
		}},
	}

	thesis := &store.Paper{
		Key: "voss_2004", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "phdthesis", Fields: map[string]string{
			"author": `Vo{\ss}, Jochen`,
			"title":  "Some large deviation results for diffusion processes",
			"school": `Universit{\"a}t Kaiserslautern`,
			"year":   "2004",
		}},
	}

	for _, p := range []*store.Paper{article, draft, thesis} {
		if err := s.Save(p); err != nil {
			t.Fatalf("saving fixture %q: %v", p.Key, err)
		}
	}

	// Stage 1: "paper check" over the whole store reports the unknown
	// macro in smith_2020's title and fails.
	var stage1Err error
	stage1Out := captureStdout(t, func() { stage1Err = runCheck(nil) })
	if stage1Err == nil {
		t.Fatal("stage 1: runCheck(nil) succeeded, want an error for the unknown macro")
	}
	if !strings.Contains(stage1Out, `smith_2020: error: bibtex.fields.title: unknown macro \badmacro`) {
		t.Errorf("stage 1: missing unknown-macro problem line for smith_2020:\n%s", stage1Out)
	}
	if strings.Contains(stage1Out, "promoted from draft to clean") {
		t.Errorf("stage 1: draft must not be promoted while it still has an error:\n%s", stage1Out)
	}

	// Stage 2: fix the offending field directly via store.Load/Save, as
	// an agent would.
	p, err := s.Load("smith_2020")
	if err != nil {
		t.Fatalf("stage 2: loading smith_2020: %v", err)
	}
	p.Bibtex.Fields["title"] = "A study of random fields"
	if err := s.Save(p); err != nil {
		t.Fatalf("stage 2: saving fixed smith_2020: %v", err)
	}

	// Stage 3: "paper check" now passes and promotes the draft to clean.
	var stage3Err error
	stage3Out := captureStdout(t, func() { stage3Err = runCheck(nil) })
	if stage3Err != nil {
		t.Fatalf("stage 3: runCheck(nil) failed after fix: %v\noutput:\n%s", stage3Err, stage3Out)
	}
	if !strings.Contains(stage3Out, "smith_2020: promoted from draft to clean") {
		t.Errorf("stage 3: missing promotion line for smith_2020:\n%s", stage3Out)
	}

	got, err := s.Load("smith_2020")
	if err != nil {
		t.Fatalf("stage 3: reloading smith_2020: %v", err)
	}
	if got.Status != "clean" {
		t.Errorf("stage 3: smith_2020 status = %q, want %q", got.Status, "clean")
	}

	// Stage 4: "paper search -json voss" finds the thesis, with its
	// author decoded from TeX to plain Unicode.
	var stage4Err error
	stage4Out := captureStdout(t, func() {
		stage4Err = runSearch([]string{"-json", "voss"})
	})
	if stage4Err != nil {
		t.Fatalf("stage 4: runSearch: %v\noutput:\n%s", stage4Err, stage4Out)
	}

	var results []map[string]any
	if err := json.Unmarshal([]byte(stage4Out), &results); err != nil {
		t.Fatalf("stage 4: unmarshaling -json search output: %v\noutput:\n%s", err, stage4Out)
	}
	if len(results) != 1 {
		t.Fatalf("stage 4: results = %v, want exactly 1 hit", results)
	}
	if results[0]["key"] != "voss_2004" {
		t.Errorf("stage 4: hit key = %v, want %q", results[0]["key"], "voss_2004")
	}
	if results[0]["authors"] != "Voß, Jochen" {
		t.Errorf("stage 4: hit authors = %v, want %q", results[0]["authors"], "Voß, Jochen")
	}

	// Stage 5: "paper bib -all" emits all three entries, sorted by key,
	// separated by blank lines.
	var stage5Err error
	stage5Out := captureStdout(t, func() { stage5Err = runBib([]string{"-all"}) })
	if stage5Err != nil {
		t.Fatalf("stage 5: runBib: %v\noutput:\n%s", stage5Err, stage5Out)
	}

	wantHeaders := []string{
		"@article{hoeffding_1963,",
		"@misc{smith_2020,",
		"@phdthesis{voss_2004,",
	}
	var indices []int
	for _, h := range wantHeaders {
		i := strings.Index(stage5Out, h)
		if i < 0 {
			t.Fatalf("stage 5: missing entry header %q in output:\n%s", h, stage5Out)
		}
		indices = append(indices, i)
	}
	for i := 1; i < len(indices); i++ {
		if indices[i-1] >= indices[i] {
			t.Errorf("stage 5: entries not in key order, want %v in ascending index order:\n%s",
				wantHeaders, stage5Out)
		}
	}
	if strings.Count(stage5Out, "\n\n@") != 2 {
		t.Errorf("stage 5: want exactly 2 blank-line separators between 3 entries:\n%s", stage5Out)
	}
}
