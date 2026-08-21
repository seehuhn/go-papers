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

package pdfid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/pdfid/pdfidtest"
)

func TestExtractInfoAndText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pdf")
	pdfidtest.MakePDF(t, path, "Probability inequalities", "Wassily Hoeffding",
		[]string{"Probability inequalities", "Wassily Hoeffding", "University of North Carolina"},
		[]float64{24, 12, 10})
	d, err := Extract(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Probability inequalities" || d.Author != "Wassily Hoeffding" {
		t.Errorf("info = %q / %q", d.Title, d.Author)
	}
	if len(d.Pages) == 0 || !strings.Contains(d.Pages[0], "North Carolina") {
		t.Errorf("page text = %q", d.Pages)
	}
	if len(d.TopLines) == 0 || !strings.Contains(d.TopLines[0], "Probability inequalities") {
		t.Errorf("TopLines = %q, largest-font line must come first", d.TopLines)
	}
}

func TestExtractNotAPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.pdf")
	os.WriteFile(path, []byte("plain text"), 0o644)
	_, err := Extract(path, 1)
	if err == nil {
		t.Fatal("non-PDF input must error")
	}
}

// TestExtractTopLinesOrder checks that TopLines is ordered by font size
// and not by position on the page.
func TestExtractTopLinesOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pdf")
	pdfidtest.MakePDF(t, path, "", "",
		[]string{"Preprint, submitted", "The real title", "small print"},
		[]float64{9, 20, 7})
	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"The real title", "Preprint, submitted", "small print"}
	if len(d.TopLines) != len(want) {
		t.Fatalf("TopLines = %q, want %q", d.TopLines, want)
	}
	for i, w := range want {
		if d.TopLines[i] != w {
			t.Errorf("TopLines[%d] = %q, want %q", i, d.TopLines[i], w)
		}
	}
}

func TestExtractNoText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank.pdf")
	pdfidtest.MakePDF(t, path, "", "", nil, nil)
	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pages) != 1 || d.Pages[0] != "" {
		t.Errorf("Pages = %q, want one empty string", d.Pages)
	}
	if d.Title != "" || d.Author != "" {
		t.Errorf("info = %q / %q, want empty", d.Title, d.Author)
	}
}
