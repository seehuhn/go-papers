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

	"seehuhn.de/go/xmp"

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
	if len(d.TopSizes) != len(d.TopLines) {
		t.Fatalf("TopSizes = %v, want same length as TopLines %q", d.TopSizes, d.TopLines)
	}
	if d.TopSizes[0] != 24 {
		t.Errorf("TopSizes[0] = %v, want 24 (the largest font size)", d.TopSizes[0])
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
	wantSizes := []float64{20, 9, 7}
	if len(d.TopLines) != len(want) {
		t.Fatalf("TopLines = %q, want %q", d.TopLines, want)
	}
	for i, w := range want {
		if d.TopLines[i] != w {
			t.Errorf("TopLines[%d] = %q, want %q", i, d.TopLines[i], w)
		}
	}
	if len(d.TopSizes) != len(wantSizes) {
		t.Fatalf("TopSizes = %v, want %v", d.TopSizes, wantSizes)
	}
	for i, w := range wantSizes {
		if d.TopSizes[i] != w {
			t.Errorf("TopSizes[%d] = %v, want %v", i, d.TopSizes[i], w)
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

// TestExtractXMP checks that a document-level XMP packet is serialised
// into DocText.XMP and that dc:title is picked out into DocText.XMPTitle.
func TestExtractXMP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xmp.pdf")
	packet := pdfidtest.DublinCorePacket(t, "doi:10.1214/aop/1176996548", "A study of SPDEs in Greenland")
	pdfidtest.MakePDF(t, path, "", "", []string{"body text"}, nil, pdfidtest.WithXMP(packet))

	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.XMP, "10.1214/aop/1176996548") {
		t.Errorf("XMP = %q, want it to contain the dc:identifier DOI", d.XMP)
	}
	if d.XMPTitle != "A study of SPDEs in Greenland" {
		t.Errorf("XMPTitle = %q, want the dc:title text", d.XMPTitle)
	}
}

// TestXMPTextCap checks that the joined text collected from an oversized
// XMP packet is truncated to maxXMPBytes instead of being kept in full or
// dropped.
func TestXMPTextCap(t *testing.T) {
	packet := xmp.NewPacket()
	err := packet.Set(&xmp.DublinCore{
		Identifier: xmp.NewText("doi:10.1214/aop/1176996548"),
		Source:     xmp.NewText(strings.Repeat("x", 2*maxXMPBytes)),
	})
	if err != nil {
		t.Fatal(err)
	}

	text := xmpText(packet)
	if len(text) > maxXMPBytes {
		t.Errorf("len(XMP) = %d, want at most the cap %d", len(text), maxXMPBytes)
	}
	if !strings.Contains(text, "10.1214/aop/1176996548") {
		t.Error("the capped text must keep the dc:identifier property (sorts before dc:source)")
	}
}

// TestXMPTitleSurvivesMalformedProperty checks that xmpTitle still returns
// dc:title when some other Dublin Core property in the same packet fails
// to decode. Packet.Get populates every field it can and only zeroes the
// ones that failed, joining the per-property errors via errors.Join; the
// caller must not let that error discard an otherwise good title.  A
// dc:date written as a bare string (instead of the required ordered
// array) is a real-world example of such a malformed property.
func TestXMPTitleSurvivesMalformedProperty(t *testing.T) {
	const body = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">A Perfectly Good Title</rdf:li></rdf:Alt></dc:title>` +
		`<dc:date>not-an-ordered-array</dc:date>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>` +
		`<?xpacket end="r"?>`
	p, err := xmp.Read(strings.NewReader(body))
	if err != nil {
		t.Fatalf("xmp.Read: %v", err)
	}

	// sanity check: confirm the premise that Get reports an error here.
	var dc xmp.DublinCore
	if err := p.Get(&dc); err == nil {
		t.Fatal("Get did not report an error for the malformed dc:date; test premise is stale")
	}

	if got := xmpTitle(p); got != "A Perfectly Good Title" {
		t.Errorf("xmpTitle = %q, want the title despite the malformed dc:date", got)
	}
}

// TestExtractNoXMP checks that a PDF without a metadata stream leaves the
// XMP fields empty rather than failing.
func TestExtractNoXMP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.pdf")
	pdfidtest.MakePDF(t, path, "Some title", "Some author", []string{"body text"}, nil)

	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.XMP != "" || d.XMPTitle != "" {
		t.Errorf("XMP = %q, XMPTitle = %q, want both empty", d.XMP, d.XMPTitle)
	}
	if d.Title != "Some title" {
		t.Errorf("Title = %q, want the Info-dict title", d.Title)
	}
}
