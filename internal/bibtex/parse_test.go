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
	got, errs := Parse(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
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
	got, errs := Parse(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
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
	got, errs := Parse(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
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
		got, errs := Parse(strings.NewReader(in))
		if len(errs) != 0 {
			t.Fatalf("Parse(month = %s): %v", macro, errs)
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
	got, errs := Parse(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
	}
	if got[0].Entry.Fields["month"] != "Custom March" {
		t.Errorf("month = %q, want %q (user @string should override the built-in)", got[0].Entry.Fields["month"], "Custom March")
	}
}

func TestParseUnknownMacroIsAnError(t *testing.T) {
	_, errs := Parse(strings.NewReader("@article{k,\n  journal = nosuchmacro,\n}"))
	if len(errs) == 0 {
		t.Fatal("Parse accepted an undefined macro, want an error")
	}
	if !strings.Contains(errs[0].Msg, "nosuchmacro") {
		t.Errorf("error %q should name the macro", errs[0].Msg)
	}
	if errs[0].Line != 2 {
		t.Errorf("Line = %d, want 2", errs[0].Line)
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
	got, errs := Parse(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
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
	_, errs := Parse(strings.NewReader("@inproceedings{child,\n  crossref = {ghost},\n}"))
	if len(errs) == 0 || !strings.Contains(errs[0].Msg, "ghost") {
		t.Errorf("got %v, want an error naming the missing parent", errs)
	}
}

func TestParseGoldenSample(t *testing.T) {
	f, err := os.Open("testdata/audit-sample.bib")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, errs := Parse(f)
	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
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
	again, errs2 := Parse(strings.NewReader(buf.String()))
	if len(errs2) != 0 {
		t.Fatalf("re-parsing formatted output: %v", errs2)
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

func TestParseRecoversAfterAMalformedEntry(t *testing.T) {
	in := `@article{good_one,
  title = {First},
  year = {1999},
}
@article{broken,
  journal = nosuchmacro,
}
@article{good_two,
  title = {Second},
  year = {2001},
}`
	got, errs := Parse(strings.NewReader(in))

	if len(got) != 2 || got[0].Key != "good_one" || got[1].Key != "good_two" {
		keys := make([]string, len(got))
		for i, e := range got {
			keys[i] = e.Key
		}
		t.Errorf("entries = %v, want [good_one good_two]", keys)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
	if errs[0].Line != 6 {
		t.Errorf("Line = %d, want 6 (the undefined macro)", errs[0].Line)
	}
	if !strings.Contains(errs[0].Msg, "nosuchmacro") {
		t.Errorf("Msg = %q, should name the macro", errs[0].Msg)
	}
}

func TestParseKeepsAChildWhoseCrossrefParentIsMissing(t *testing.T) {
	in := `@inproceedings{child,
  title = {A talk},
  crossref = {ghost},
}`
	got, errs := Parse(strings.NewReader(in))

	if len(got) != 1 || got[0].Key != "child" {
		t.Fatalf("entries = %v, want the child kept", got)
	}
	if got[0].Entry.Fields["title"] != "A talk" {
		t.Errorf("the child's own fields must survive: %v", got[0].Entry.Fields)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Msg, "ghost") {
		t.Errorf("errs = %v, want one error naming the missing parent", errs)
	}
}

func TestParseReportsAnUnbalancedBraceInAQuotedValue(t *testing.T) {
	in := "@article{k,\n  note = \"a } b\",\n  year = {1999},\n}"

	_, errs := Parse(strings.NewReader(in))

	if len(errs) == 0 {
		t.Fatal("an unbalanced '}' inside a quoted value should be an error")
	}
	if errs[0].Line != 2 {
		t.Errorf("Line = %d, want 2 (where the '}' is)", errs[0].Line)
	}
	if !strings.Contains(errs[0].Msg, "}") {
		t.Errorf("Msg = %q, should name the unbalanced brace", errs[0].Msg)
	}
}

func TestParseAcceptsParenthesisDelimitedEntries(t *testing.T) {
	in := `@article(hoef,
  author = {Hoeffding, Wassily},
  title = {Probability inequalities},
  year = 1963,
)
@string(jams = {J. Amer. Math. Soc.})
@article(k2,
  journal = jams,
)`
	got, errs := Parse(strings.NewReader(in))

	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Entry.Fields["author"] != "Hoeffding, Wassily" {
		t.Errorf("author = %q", got[0].Entry.Fields["author"])
	}
	if got[1].Entry.Fields["journal"] != "J. Amer. Math. Soc." {
		t.Errorf("@string in paren form should work too: journal = %q", got[1].Entry.Fields["journal"])
	}
}

func TestParseParenEntryIsClosedByParenNotBrace(t *testing.T) {
	// A '}' does not close a paren-delimited entry; braces inside values
	// still work, and the entry ends only at ')'.
	in := "@article(k,\n  title = {Braced {T}itle},\n)"

	got, errs := Parse(strings.NewReader(in))

	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
	}
	if got[0].Entry.Fields["title"] != "Braced {T}itle" {
		t.Errorf("title = %q", got[0].Entry.Fields["title"])
	}
}

func TestParseParenCommentDoesNotSwallowTheNextEntry(t *testing.T) {
	in := "@comment( ignore all this )\n@article{k,\n  title = {Kept},\n}"

	got, errs := Parse(strings.NewReader(in))

	if len(errs) != 0 {
		t.Fatalf("Parse: %v", errs)
	}
	if len(got) != 1 || got[0].Entry.Fields["title"] != "Kept" {
		t.Errorf("the entry after a paren comment must survive, got %v", got)
	}
}

// TestParseNamesTheLineForUnterminatedInput pins the line numbers of the
// three truncation errors. These were hand-verified during review; the
// table keeps the line-tracking invariant in next() honest.
func TestParseNamesTheLineForUnterminatedInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
		msg  string
	}{
		{"entry", "@article{k,\n  title = {T},\n", 1, "unterminated entry"},
		{"brace", "@article{k,\n  title = {no end\n", 2, "unterminated brace group"},
		{"quote", "@article{k,\n\n  note = \"no end\n", 3, "unterminated quoted value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := Parse(strings.NewReader(c.in))
			if len(errs) == 0 {
				t.Fatal("want an error")
			}
			if errs[0].Line != c.line {
				t.Errorf("Line = %d, want %d", errs[0].Line, c.line)
			}
			if !strings.Contains(errs[0].Msg, c.msg) {
				t.Errorf("Msg = %q, want it to contain %q", errs[0].Msg, c.msg)
			}
		})
	}
}

func TestParseRecoveryDoesNotEatAnEntryStartingAtTheFailure(t *testing.T) {
	// The broken entry is missing a comma/brace, so the field loop's
	// skipSpace stops exactly on the next entry's '@' before failing.
	in := "@article{broken,\n  title = {Something}\n@article{good,\n  title = {x},\n}"

	got, errs := Parse(strings.NewReader(in))

	if len(got) != 1 || got[0].Key != "good" {
		t.Errorf("entries = %v, want [good]", got)
	}
	if len(errs) != 1 {
		t.Errorf("errs = %v, want exactly one (for broken)", errs)
	}
}
