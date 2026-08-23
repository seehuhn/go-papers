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
	"maps"
	"slices"
	"testing"

	"golang.org/x/text/language"

	"seehuhn.de/go/xmp"

	"seehuhn.de/go/pdf"
	"seehuhn.de/go/pdf/document"
	"seehuhn.de/go/pdf/font/gofont"
)

// defaultSize is the font size used for lines not covered by the sizes slice.
const defaultSize = 10.0

// Option modifies the PDF written by [MakePDF].
type Option func(*pdf.WriterOptions)

// WithXMP attaches p to the generated document as the document-level XMP
// metadata stream.  The stream is written in plaintext, i.e. uncompressed,
// which makes the packet visible to byte-scanning tools as well.
func WithXMP(p *xmp.Packet) Option {
	return func(opt *pdf.WriterOptions) {
		opt.DocumentMetadata = &pdf.MetadataStream{Data: p, Plaintext: true}
	}
}

// The XMP namespace URIs the PRISM fixtures use.  Publishers embed all
// four PRISM Basic versions in the wild; PDFXNS is the Adobe "custom
// document properties" namespace Springer and others use for pdfx:doi.
const (
	PrismNS12 = "http://prismstandard.org/namespaces/1.2/basic/"
	PrismNS20 = "http://prismstandard.org/namespaces/basic/2.0/"
	PrismNS21 = "http://prismstandard.org/namespaces/basic/2.1/"
	PrismNS30 = "http://prismstandard.org/namespaces/basic/3.0/"
	PDFXNS    = "http://ns.adobe.com/pdfx/1.3/"
)

// SetProperties sets simple text properties in namespace ns on p, keyed
// by property name (e.g. "doi", "publicationName").  Properties with an
// empty value are skipped, so a fixture can list a field it does not
// want to set.  Use it to layer a second namespace onto a packet built
// by [PrismPacket] or [DublinCorePacket].
func SetProperties(t *testing.T, p *xmp.Packet, ns string, props map[string]string) {
	t.Helper()

	for _, name := range slices.Sorted(maps.Keys(props)) {
		if props[name] == "" {
			continue
		}
		if err := p.SetValue(ns, name, xmp.NewText(props[name])); err != nil {
			t.Fatalf("cannot set %s %s: %v", ns, name, err)
		}
	}
}

// PrismPacket returns an XMP packet carrying the given PRISM Basic
// properties in namespace ns (one of [PrismNS12], [PrismNS20], [PrismNS21],
// [PrismNS30]).  The keys are bare PRISM property names, e.g.
// map[string]string{"doi": "10.1234/x", "volume": "12"}.
func PrismPacket(t *testing.T, ns string, props map[string]string) *xmp.Packet {
	t.Helper()

	p := xmp.NewPacket()
	p.RegisterPrefix(ns, "prism")
	SetProperties(t, p, ns, props)
	return p
}

// DublinCorePacket returns an XMP packet carrying the given dc:identifier
// and dc:title (each omitted when empty).  The title is stored as the
// "x-default" entry of the language alternative.
func DublinCorePacket(t *testing.T, identifier, title string) *xmp.Packet {
	t.Helper()

	dc := &xmp.DublinCore{}
	if identifier != "" {
		dc.Identifier = xmp.NewText(identifier)
	}
	if title != "" {
		dc.Title.Set(language.Und, title)
	}

	p := xmp.NewPacket()
	if err := p.Set(dc); err != nil {
		t.Fatalf("cannot set Dublin Core properties: %v", err)
	}
	return p
}

// MakePDF writes a single-page PDF with the given Info-dict title,
// author, and body lines.  Lines are rendered top to bottom; sizes[i]
// gives the font size of lines[i] (default 10 where sizes is short).
// Empty title/author leave the Info entries unset; empty lines produce
// a page with no text.  Options, e.g. [WithXMP], add optional document
// features.
func MakePDF(t *testing.T, path, title, author string, lines []string, sizes []float64, opts ...Option) {
	t.Helper()

	var writerOpt *pdf.WriterOptions
	if len(opts) > 0 {
		writerOpt = &pdf.WriterOptions{}
		for _, opt := range opts {
			opt(writerOpt)
		}
	}

	doc, err := document.CreateSinglePage(path, document.A4, pdf.V2_0, writerOpt)
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
