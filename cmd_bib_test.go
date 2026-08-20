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
	"strings"
	"testing"
)

func TestBibSingleKey(t *testing.T) {
	s, _ := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	out := captureStdout(t, func() {
		if err := runBib([]string{"hoeffding_1963"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(out, "@article{hoeffding_1963,") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "  doi = {10.1080/01621459.1963.10500830},") {
		t.Errorf("missing doi line:\n%s", out)
	}
}

func TestBibUnknownKey(t *testing.T) {
	fixtureStore(t)
	if err := runBib([]string{"nope_1900"}); err == nil {
		t.Error("expected error")
	}
}

func TestBibMultipleKeys(t *testing.T) {
	s, _ := fixtureStore(t)
	p1 := cleanPaper("hoeffding_1963")
	s.Save(p1)

	p2 := cleanPaper("abc_1964")
	p2.Bibtex.Fields["author"] = "Author, Another"
	p2.Bibtex.Fields["year"] = "1964"
	s.Save(p2)

	// Request keys out of order; should be sorted in output
	out := captureStdout(t, func() {
		if err := runBib([]string{"hoeffding_1963", "abc_1964"}); err != nil {
			t.Fatal(err)
		}
	})

	// Check both entries are present in sorted order
	if !strings.HasPrefix(out, "@article{abc_1964,") {
		t.Errorf("first entry should be abc_1964 (sorted), got:\n%s", out)
	}

	// Check that hoeffding comes after abc
	abcIdx := strings.Index(out, "@article{abc_1964,")
	hoeffIdx := strings.Index(out, "@article{hoeffding_1963,")
	if abcIdx > hoeffIdx {
		t.Errorf("entries not sorted: abc should come before hoeffding:\n%s", out)
	}
}

func TestBibAllFlag(t *testing.T) {
	s, _ := fixtureStore(t)
	p1 := cleanPaper("hoeffding_1963")
	s.Save(p1)

	p2 := cleanPaper("abc_1964")
	p2.Bibtex.Fields["author"] = "Author, Another"
	p2.Bibtex.Fields["year"] = "1964"
	s.Save(p2)

	out := captureStdout(t, func() {
		if err := runBib([]string{"-all"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "@article{abc_1964,") {
		t.Errorf("missing abc_1964 in -all output:\n%s", out)
	}
	if !strings.Contains(out, "@article{hoeffding_1963,") {
		t.Errorf("missing hoeffding_1963 in -all output:\n%s", out)
	}
}

func TestBibBlankLineSeparation(t *testing.T) {
	s, _ := fixtureStore(t)
	p1 := cleanPaper("hoeffding_1963")
	s.Save(p1)

	p2 := cleanPaper("abc_1964")
	p2.Bibtex.Fields["author"] = "Author, Another"
	p2.Bibtex.Fields["year"] = "1964"
	s.Save(p2)

	out := captureStdout(t, func() {
		if err := runBib([]string{"-all"}); err != nil {
			t.Fatal(err)
		}
	})

	// Should have a blank line between entries
	if !strings.Contains(out, "}\n\n@") {
		t.Errorf("expected blank line between entries:\n%s", out)
	}
}

func TestBibAllWithKeys(t *testing.T) {
	fixtureStore(t)
	if err := runBib([]string{"-all", "hoeffding_1963"}); err == nil {
		t.Error("expected error when combining -all with keys")
	}
}

func TestBibNoArgs(t *testing.T) {
	fixtureStore(t)
	if err := runBib([]string{}); err == nil {
		t.Error("expected error when neither -all nor keys given")
	}
}
