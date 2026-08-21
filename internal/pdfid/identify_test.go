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
	"path/filepath"
	"testing"

	"seehuhn.de/go/paper/internal/pdfid/pdfidtest"
)

func TestIdentifyTier1CustomDOI(t *testing.T) {
	d := &DocText{Custom: map[string]string{"doi": "10.1080/01621459.1963.10500830"}}
	id := Identify(d, nil)
	if id.Tier != 1 || id.DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("id = %+v", id)
	}
}

func TestIdentifyTier2ArxivStamp(t *testing.T) {
	d := &DocText{Pages: []string{"arXiv:2412.05039v2 [math.PR] 6 Dec 2024\nA study of SPDEs"}}
	id := Identify(d, nil)
	if id.Tier != 2 || id.ArxivID != "2412.05039" || id.Version != 2 {
		t.Errorf("id = %+v", id)
	}
}

func TestIdentifyTier2DOIInText(t *testing.T) {
	d := &DocText{Pages: []string{"", "DOI: 10.1214/aop/1176996548."}}
	id := Identify(d, nil)
	if id.Tier != 2 || id.DOI != "10.1214/aop/1176996548" {
		t.Errorf("trailing dot must be trimmed: %+v", id)
	}
}

func TestIdentifyTier3(t *testing.T) {
	d := &DocText{
		Pages:    []string{"Probability inequalities for sums of bounded random variables\nWassily Hoeffding"},
		TopLines: []string{"Probability inequalities for sums of bounded random variables", "Wassily Hoeffding"},
	}
	search := func(guess string) (string, string, float64, error) {
		return "10.1080/01621459.1963.10500830",
			"Probability Inequalities for Sums of Bounded Random Variables", 1.0, nil
	}
	id := Identify(d, search)
	if id.Tier != 3 || id.DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("id = %+v", id)
	}
}

func TestIdentifyTier3LowScoreRejected(t *testing.T) {
	d := &DocText{
		Pages:    []string{"Some obscure heading"},
		TopLines: []string{"Some obscure heading text here"},
	}
	search := func(guess string) (string, string, float64, error) {
		return "10.9999/wrong", "Completely Different Paper", 0.1, nil
	}
	id := Identify(d, search)
	if id.Tier != 0 || id.DOI != "" {
		t.Errorf("low similarity must not identify: %+v", id)
	}
	if id.Title == "" {
		t.Error("the rejected guess must still be reported in Title")
	}
}

func TestIdentifyTier3JoinsSameSizeLines(t *testing.T) {
	d := &DocText{
		TopLines: []string{"A study of stochastic partial", "differential equations in Greenland", "Jochen Voss"},
		TopSizes: []float64{20, 20, 12},
	}
	const want = "A study of stochastic partial differential equations in Greenland"
	search := func(guess string) (string, string, float64, error) {
		if guess != want {
			t.Errorf("guess = %q, want the joined title %q", guess, want)
		}
		return "10.1234/greenland-spdes", want, 1.0, nil
	}
	id := Identify(d, search)
	if id.Tier != 3 || id.Title != want {
		t.Errorf("id = %+v", id)
	}
}

func TestIdentifyTier1SubjectDOI(t *testing.T) {
	d := &DocText{Subject: "doi:10.1214/aop/1176996548"}
	id := Identify(d, nil)
	if id.Tier != 1 || id.DOI != "10.1214/aop/1176996548" {
		t.Errorf("id = %+v", id)
	}
}

func TestIdentifyScanned(t *testing.T) {
	d := &DocText{Pages: []string{"", ""}}
	id := Identify(d, nil)
	if !id.Scanned || id.Tier != 0 {
		t.Errorf("id = %+v", id)
	}
}

// TestIdentifyTier1XMPDOI checks the end-to-end path for a DOI which is
// only present in the document's XMP metadata stream: the packet is
// serialised by Extract and the tier-1 regex scan finds the DOI there.
func TestIdentifyTier1XMPDOI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xmp.pdf")
	packet := pdfidtest.DublinCorePacket(t, "doi:10.1080/01621459.1963.10500830", "")
	pdfidtest.MakePDF(t, path, "", "", []string{"body text without any identifier"}, nil,
		pdfidtest.WithXMP(packet))

	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := Identify(d, nil)
	if id.Tier != 1 || id.DOI != "10.1080/01621459.1963.10500830" {
		t.Errorf("id = %+v, want the DOI from the XMP packet at tier 1", id)
	}
}

// TestIdentifyTier1XMPArxiv checks that an arXiv stamp in the XMP packet
// is found by tier 1 as well.
func TestIdentifyTier1XMPArxiv(t *testing.T) {
	d := &DocText{XMP: `<dc:description>arXiv:2412.05039v2</dc:description>`}
	id := Identify(d, nil)
	if id.Tier != 1 || id.ArxivID != "2412.05039" || id.Version != 2 {
		t.Errorf("id = %+v", id)
	}
}

// TestIdentifyTier3InfoTitleFallback checks the tier-3 fallback to the
// Info-dict title when the page text yields no usable title guess.
func TestIdentifyTier3InfoTitleFallback(t *testing.T) {
	const want = "Probability inequalities for sums of bounded random variables"
	d := &DocText{Title: want, Pages: []string{""}}
	search := func(guess string) (string, string, float64, error) {
		if guess != want {
			t.Errorf("guess = %q, want the Info-dict title %q", guess, want)
		}
		return "10.1080/01621459.1963.10500830", want, 1.0, nil
	}
	id := Identify(d, search)
	if id.Tier != 3 || id.DOI != "10.1080/01621459.1963.10500830" || id.Title != want {
		t.Errorf("id = %+v", id)
	}
}

// TestIdentifyTier3XMPTitleFallback checks that dc:title is used when
// neither the page text nor the Info dictionary offers a title.
func TestIdentifyTier3XMPTitleFallback(t *testing.T) {
	const want = "A study of SPDEs in Greenland"
	d := &DocText{XMPTitle: want, Pages: []string{""}}
	search := func(guess string) (string, string, float64, error) {
		if guess != want {
			t.Errorf("guess = %q, want the dc:title %q", guess, want)
		}
		return "10.1234/greenland-spdes", want, 1.0, nil
	}
	id := Identify(d, search)
	if id.Tier != 3 || id.DOI != "10.1234/greenland-spdes" {
		t.Errorf("id = %+v", id)
	}
}

// TestIdentifyTier3MetadataTitleRejected checks that a metadata-derived
// guess is still subject to the similarity gate, and that the rejected
// guess is reported in ID.Title.
func TestIdentifyTier3MetadataTitleRejected(t *testing.T) {
	d := &DocText{Title: "Microsoft Word - draft17.doc", Pages: []string{""}}
	search := func(guess string) (string, string, float64, error) {
		return "10.9999/wrong", "Completely Different Paper", 0.1, nil
	}
	id := Identify(d, search)
	if id.Tier != 0 || id.DOI != "" {
		t.Errorf("low similarity must not identify: %+v", id)
	}
	if id.Title != "Microsoft Word - draft17.doc" {
		t.Errorf("Title = %q, want the rejected guess", id.Title)
	}
}

// TestIdentifyTier3NilSearchSkipsMetadataTitle checks that the nil-search
// skip still applies when only a metadata title is available.
func TestIdentifyTier3NilSearchSkipsMetadataTitle(t *testing.T) {
	d := &DocText{Title: "Some title", XMPTitle: "Some other title", Pages: []string{""}}
	id := Identify(d, nil)
	if id.Tier != 0 || id.Title != "" {
		t.Errorf("id = %+v, want no tier-3 attempt without a search function", id)
	}
}
