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

// Package pdfid extracts identifying information from PDF files: the
// document information dictionary and the text of the first few pages.
package pdfid

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"seehuhn.de/go/postscript/cid"
	"seehuhn.de/go/postscript/type1/names"

	"seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/font"
	"seehuhn.de/go/pdf/font/pdfenc"
	"seehuhn.de/go/pdf/font/textextract"
	"seehuhn.de/go/pdf/page"
	"seehuhn.de/go/pdf/pagetree"
	"seehuhn.de/go/pdf/reader"
)

// DocText is the extraction result for identification purposes.
type DocText struct {
	Title    string            // Info dict Title, "" if unset
	Author   string            // Info dict Author
	Subject  string            // Info dict Subject, "" if unset
	Keywords string            // Info dict Keywords, "" if unset
	Custom   map[string]string // non-standard Info entries (doi often hides here)
	Pages    []string          // plain text of the first pages requested
	// TopLines holds the first page's text lines ordered by descending
	// font size: the largest-font run(s) first.  Used for title guessing.
	// TopSizes is the parallel slice of font sizes: TopSizes[i] is the
	// font size of TopLines[i].  The two slices always have equal length.
	TopLines []string
	TopSizes []float64
}

// Extract opens the PDF at path and extracts the info dictionary and the
// text of up to maxPages pages.  A file that is not a PDF, or is
// encrypted, returns an error saying so.  A PDF whose pages yield no text
// returns Pages entries that are empty strings (the caller detects the
// scanned-PDF case).
func Extract(path string, maxPages int) (*DocText, error) {
	r, err := pdf.Open(path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot read PDF %s: %w", path, err)
	}
	defer r.Close()

	res := &DocText{}
	if info := r.GetMeta().Info; info != nil {
		res.Title = string(info.Title)
		res.Author = string(info.Author)
		res.Subject = string(info.Subject)
		res.Keywords = string(info.Keywords)
		if len(info.Custom) > 0 {
			res.Custom = maps.Clone(info.Custom)
		}
	}

	if maxPages <= 0 {
		return res, nil
	}

	e := newExtractor(r)
	tree := pagetree.NewIterator(r)

	// A malformed content stream should not prevent identification from
	// the Info dictionary or from the pages which could be read.  We
	// remember the first error and only report it if no text was found
	// at all.
	var firstErr error
	numPages := 0
	for _, pageDict := range tree.All() {
		first := numPages == 0
		e.reset(first)
		if err := e.processPage(pageDict); err != nil && firstErr == nil {
			firstErr = err
		}
		res.Pages = append(res.Pages, e.text())
		if first {
			res.TopLines, res.TopSizes = e.topLines()
		}

		numPages++
		if numPages >= maxPages {
			break
		}
	}
	if tree.Err != nil && firstErr == nil {
		firstErr = tree.Err
	}

	if firstErr != nil && !hasText(res.Pages) {
		return nil, fmt.Errorf("cannot extract text from %s: %w", path, firstErr)
	}
	return res, nil
}

// hasText reports whether any of the given page texts is non-empty.
func hasText(pages []string) bool {
	for _, p := range pages {
		if p != "" {
			return true
		}
	}
	return false
}

// sizedLine is a line of text together with the largest font size used in
// the line.
type sizedLine struct {
	text string
	size float64
}

// extractor collects the text of a PDF page.  It is modelled on the
// TextExtractor in seehuhn.de/go/pdf/cmd/pdf-extract.
type extractor struct {
	r *reader.Reader
	x *pdf.Extractor

	// buf collects the text of the current page.
	buf strings.Builder

	// glyphNames caches the glyph-name-based fallback mappings, keyed by
	// font.  The mapping only depends on the font, so the cache can be
	// kept across pages.
	glyphNames map[font.Instance]map[cid.CID]string

	// lastWasWhitespace tracks whether the most recently emitted character
	// was whitespace, so that an adjacent space can be collapsed.
	// lastWasNewline narrows that to specifically a newline, so that
	// adjacent newlines collapse.  Both start true to suppress leading
	// whitespace.
	lastWasWhitespace bool
	lastWasNewline    bool

	// trackLines enables the per-line bookkeeping used for TopLines.
	trackLines bool
	line       strings.Builder
	lineSize   float64
	lines      []sizedLine
}

// newExtractor returns an extractor reading from r.
func newExtractor(r pdf.Getter) *extractor {
	x := pdf.NewExtractor(r)
	e := &extractor{
		r:          reader.New(x),
		x:          x,
		glyphNames: make(map[font.Instance]map[cid.CID]string),
	}

	e.r.ActualText = func(event reader.ActualTextEvent, text string) error {
		if event == reader.ActualTextBegin {
			e.writeText(text)
		}
		return nil
	}

	e.r.TextEvent = func(event reader.TextEvent, _ float64) {
		switch event {
		case reader.TextEventSpace:
			e.writeSpace()
		case reader.TextEventNL:
			e.writeNewline()
		}
	}

	e.r.Character = func(c font.Code) error {
		if e.trackLines {
			// The effective font size is the text font size scaled
			// through the text and current transformation matrices.
			trm := e.r.State.GState.TextRenderingMatrix()
			size := math.Hypot(trm[2], trm[3])
			if size > e.lineSize {
				e.lineSize = size
			}
		}

		// inside an ActualText region the replacement text has already
		// been emitted; suppress per-glyph text
		if e.r.InActualText() {
			return nil
		}

		text := c.Text
		if text == "" {
			currentFont := e.r.State.GState.TextFont
			mapping, ok := e.glyphNames[currentFont]
			if !ok {
				mapping = textextract.GlyphNameMapping(currentFont)
				e.glyphNames[currentFont] = mapping
			}
			text = mapping[c.CID]
		}

		e.writeText(remapPUA(text))
		return nil
	}

	return e
}

// reset prepares the extractor for a new page.  If trackLines is true, the
// per-line font sizes needed for [extractor.topLines] are recorded.
func (e *extractor) reset(trackLines bool) {
	e.buf.Reset()
	e.lastWasWhitespace = true
	e.lastWasNewline = true

	e.trackLines = trackLines
	e.line.Reset()
	e.lineSize = 0
	e.lines = nil
}

// processPage extracts the text of the given page dictionary.
func (e *extractor) processPage(pageDict pdf.Dict) error {
	pg, err := pdf.Decode(pdf.CursorAt(e.x, nil), pageDict, page.Decode)
	if err != nil {
		return err
	}
	return e.r.ProcessPage(pg)
}

// text returns the text of the page most recently processed.
func (e *extractor) text() string {
	return strings.TrimSpace(e.buf.String())
}

// topLines returns the lines of the page most recently processed, ordered
// by descending font size, together with the font size of each line in
// the parallel sizes slice (sizes[i] is the font size of lines[i]).
// Lines of equal size keep their original order.
func (e *extractor) topLines() (lines []string, sizes []float64) {
	e.flushLine()
	ls := slices.Clone(e.lines)
	slices.SortStableFunc(ls, func(a, b sizedLine) int {
		return cmp.Compare(b.size, a.size)
	})

	lines = make([]string, len(ls))
	sizes = make([]float64, len(ls))
	for i, l := range ls {
		lines[i] = l.text
		sizes[i] = l.size
	}
	return lines, sizes
}

// writeSpace emits a space, collapsing it against any preceding whitespace.
func (e *extractor) writeSpace() {
	if e.lastWasWhitespace {
		return
	}
	e.buf.WriteString(" ")
	if e.trackLines {
		e.line.WriteString(" ")
	}
	e.lastWasWhitespace = true
	e.lastWasNewline = false
}

// writeNewline emits a newline and ends the current line.
func (e *extractor) writeNewline() {
	e.flushLine()
	if e.lastWasNewline {
		return
	}
	e.buf.WriteString("\n")
	e.lastWasWhitespace = true
	e.lastWasNewline = true
}

// writeText emits character text from the content stream.  An empty text
// is ignored.  A single-space text collapses against preceding whitespace.
func (e *extractor) writeText(text string) {
	if text == "" {
		return
	}
	if text == " " {
		e.writeSpace()
		return
	}
	e.buf.WriteString(text)
	if e.trackLines {
		e.line.WriteString(text)
	}
	last := text[len(text)-1]
	e.lastWasWhitespace = last == ' ' || last == '\n' || last == '\t'
	e.lastWasNewline = last == '\n'
}

// flushLine records the current line, if any, and starts a new one.
func (e *extractor) flushLine() {
	if !e.trackLines {
		return
	}
	text := strings.TrimSpace(e.line.String())
	if text != "" {
		e.lines = append(e.lines, sizedLine{text: text, size: e.lineSize})
	}
	e.line.Reset()
	e.lineSize = 0
}

// remapPUA replaces Private Use Area codepoints (U+F020-U+F0FF) with their
// Unicode equivalents.  Some PDF generators map Symbol font characters to
// this PUA range instead of real Unicode.  The low byte of each PUA
// codepoint corresponds to the Symbol encoding position.
func remapPUA(text string) string {
	needsRemap := false
	for _, r := range text {
		if r >= 0xF020 && r <= 0xF0FF {
			needsRemap = true
			break
		}
	}
	if !needsRemap {
		return text
	}

	var buf []rune
	for _, r := range text {
		if r >= 0xF020 && r <= 0xF0FF {
			glyphName := pdfenc.Symbol.Encoding[r-0xF000]
			if glyphName != ".notdef" {
				replacement := names.ToUnicode(glyphName, "")
				if replacement != "" {
					buf = append(buf, []rune(replacement)...)
					continue
				}
			}
		}
		buf = append(buf, r)
	}
	return string(buf)
}
