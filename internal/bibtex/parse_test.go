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

package bibtex

import (
	"maps"
	"os"
	"strings"
	"testing"
)

func TestParseOneEntry(t *testing.T) {
	in := `@article{hoeffding_1963,
  author = {Hoeffding, Wassily},
  title = {Probability inequalities},
  year = {1963},
}`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	e := got[0]
	if e.Key != "hoeffding_1963" {
		t.Errorf("Key = %q, want %q", e.Key, "hoeffding_1963")
	}
	if e.Entry.Type != "article" {
		t.Errorf("Type = %q, want %q", e.Entry.Type, "article")
	}
	if e.Entry.Fields["author"] != "Hoeffding, Wassily" {
		t.Errorf("author = %q", e.Entry.Fields["author"])
	}
	if e.Line != 1 {
		t.Errorf("Line = %d, want 1", e.Line)
	}
}

func TestParseValueForms(t *testing.T) {
	in := `@article{k,
  author = "Vo{\ss}, Jochen",
  year = 1963,
  title = {A study of {SPDEs} in {G}reenland},
  note = {a "quoted" word},
}`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	f := got[0].Entry.Fields
	want := map[string]string{
		"author": `Vo{\ss}, Jochen`,
		"year":   "1963",
		"title":  "A study of {SPDEs} in {G}reenland",
		"note":   `a "quoted" word`,
	}
	for name, w := range want {
		if f[name] != w {
			t.Errorf("%s = %q, want %q", name, f[name], w)
		}
	}
}

func TestParseStringMacrosAndConcatenation(t *testing.T) {
	in := `@string{jams = {J. Amer. Math. Soc.}}
@string{vol = {12}}
@article{k,
  journal = jams,
  note = jams # { and more} # vol,
}`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (@string is not an entry)", len(got))
	}
	f := got[0].Entry.Fields
	if f["journal"] != "J. Amer. Math. Soc." {
		t.Errorf("journal = %q", f["journal"])
	}
	if f["note"] != "J. Amer. Math. Soc. and more12" {
		t.Errorf("note = %q", f["note"])
	}
}

// TestParseBuiltinMonthMacros pins Finding 2: bibtex's twelve built-in
// three-letter month macros (jan...dec) must be predefined, matching what
// bibtex styles render — the conventional English month name.
func TestParseBuiltinMonthMacros(t *testing.T) {
	want := map[string]string{
		"jan": "January", "feb": "February", "mar": "March", "apr": "April",
		"may": "May", "jun": "June", "jul": "July", "aug": "August",
		"sep": "September", "oct": "October", "nov": "November", "dec": "December",
	}
	for macro, expansion := range want {
		in := "@article{k,\n  month = " + macro + ",\n}"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("Parse(month = %s): %v", macro, err)
		}
		if got[0].Entry.Fields["month"] != expansion {
			t.Errorf("month = %q, want %q", got[0].Entry.Fields["month"], expansion)
		}
	}
}

// TestParseUserStringOverridesBuiltinMonthMacro pins the last-definition-
// wins rule: a user @string redefining a built-in month macro overrides
// it, matching bibtex itself.
func TestParseUserStringOverridesBuiltinMonthMacro(t *testing.T) {
	in := `@string{mar = {Custom March}}
@article{k,
  month = mar,
}`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got[0].Entry.Fields["month"] != "Custom March" {
		t.Errorf("month = %q, want %q (user @string should override the built-in)", got[0].Entry.Fields["month"], "Custom March")
	}
}

func TestParseUnknownMacroIsAnError(t *testing.T) {
	_, err := Parse(strings.NewReader("@article{k,\n  journal = nosuchmacro,\n}"))
	if err == nil {
		t.Fatal("Parse accepted an undefined macro, want an error")
	}
	if !strings.Contains(err.Error(), "nosuchmacro") {
		t.Errorf("error %q should name the macro", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %q should name the line", err)
	}
}

func TestParseCrossrefInheritance(t *testing.T) {
	in := `@inproceedings{child,
  author = {Smith, Ann},
  title = {A talk},
  crossref = {proc},
}
@proceedings{proc,
  booktitle = {Proc. Nowhere},
  year = {2011},
  publisher = {Nobody},
}`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var child KeyedEntry
	for _, e := range got {
		if e.Key == "child" {
			child = e
		}
	}
	if child.Entry.Fields["booktitle"] != "Proc. Nowhere" {
		t.Errorf("booktitle = %q, want it inherited from the parent", child.Entry.Fields["booktitle"])
	}
	if child.Entry.Fields["year"] != "2011" {
		t.Errorf("year = %q, want it inherited", child.Entry.Fields["year"])
	}
	if child.Entry.Fields["title"] != "A talk" {
		t.Errorf("title = %q, want the child's own title kept", child.Entry.Fields["title"])
	}
}

func TestParseCrossrefToAMissingParentIsAnError(t *testing.T) {
	_, err := Parse(strings.NewReader("@inproceedings{child,\n  crossref = {ghost},\n}"))
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("got %v, want an error naming the missing parent", err)
	}
}

func TestParseGoldenSample(t *testing.T) {
	f, err := os.Open("testdata/audit-sample.bib")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("got %d entries, want 6", len(got))
	}
	// Round-tripping through Format must not lose the encoding: every value
	// is already bibtex-encoded, so re-parsing the formatted output has to
	// return the same fields.
	var buf strings.Builder
	for _, e := range got {
		buf.WriteString(Format(e.Key, e.Entry))
		buf.WriteString("\n")
	}
	again, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("re-parsing formatted output: %v", err)
	}
	if len(again) != len(got) {
		t.Fatalf("round trip changed the entry count: %d -> %d", len(got), len(again))
	}
	for i := range got {
		if !maps.Equal(got[i].Entry.Fields, again[i].Entry.Fields) {
			t.Errorf("%s: fields changed on round trip:\n%v\n%v",
				got[i].Key, got[i].Entry.Fields, again[i].Entry.Fields)
		}
	}
}
