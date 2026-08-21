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

// Package pdfidtest generates small test PDFs for identification tests.
package pdfidtest

import (
	"testing"

	"seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/document"
	"seehuhn.de/go/pdf/font/gofont"
)

// defaultSize is the font size used for lines not covered by the sizes slice.
const defaultSize = 10.0

// MakePDF writes a single-page PDF with the given Info-dict title,
// author, and body lines.  Lines are rendered top to bottom; sizes[i]
// gives the font size of lines[i] (default 10 where sizes is short).
// Empty title/author leave the Info entries unset; empty lines produce
// a page with no text.
func MakePDF(t *testing.T, path, title, author string, lines []string, sizes []float64) {
	t.Helper()

	doc, err := document.CreateSinglePage(path, document.A4, pdf.V2_0, nil)
	if err != nil {
		t.Fatalf("cannot create %s: %v", path, err)
	}

	if title != "" || author != "" {
		meta := doc.Out.GetMeta()
		if meta.Info == nil {
			meta.Info = &pdf.Info{}
		}
		meta.Info.Title = pdf.TextString(title)
		meta.Info.Author = pdf.TextString(author)
	}

	if len(lines) > 0 {
		font, err := gofont.Regular.NewSimple(nil)
		if err != nil {
			t.Fatalf("cannot load font: %v", err)
		}

		// Draw each line in its own text object, so that changing the
		// font size between lines is easy.  The vertical gap is larger
		// than half the font size, so that the reader reports a line
		// break between consecutive lines.
		y := document.A4.URy - 72
		for i, line := range lines {
			size := defaultSize
			if i < len(sizes) {
				size = sizes[i]
			}
			doc.TextSetFont(font, size)
			doc.TextBegin()
			doc.TextFirstLine(72, y)
			doc.TextShow(line)
			doc.TextEnd()
			y -= 2 * size
		}
	}

	if err := doc.Close(); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}
