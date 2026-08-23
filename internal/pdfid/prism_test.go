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
	"reflect"
	"strings"
	"testing"

	"seehuhn.de/go/xmp"

	"seehuhn.de/go/paper/internal/pdfid/pdfidtest"
)

// TestPrismModelTagsAgree is the guard the struct conversions in
// prism.go cannot be: it compares the three PRISM Basic models property
// by property.
//
// The conversions to prismValues already stop the package building if a
// field is added, removed, renamed or retyped in one copy alone. They
// stop nothing else, because Go ignores struct tags when checking type
// identity for a conversion. Misspelling a tag in one copy - `eissn` for
// `eIssn` in the 2.0 model, say - compiles cleanly and silently yields
// an absent property for that namespace version, which is invisible to
// every other test here: TestPrismModelRoundTrip exercises all thirteen
// tags but only for 2.1, and TestPrismNamespaceVersions checks only
// doi/publicationName/volume for 2.0 and 3.0.
//
// So: the (field name, xmp tag) sequences must be identical across the
// three, every property tag must be present (an empty tag makes go-xmp
// fall back to the Go field name, so "DOI" would be looked up instead of
// "doi"), and each model must carry the namespace URI of its own
// version - a copy-paste leaving 3.0 on the 2.1 URI would make
// readPrismBasic read the same namespace twice and never see 3.0 at all.
func TestPrismModelTagsAgree(t *testing.T) {
	models := []struct {
		name string
		typ  reflect.Type
		ns   string
	}{
		{"prismBasic21", reflect.TypeFor[prismBasic21](), pdfidtest.PrismNS21},
		{"prismBasic20", reflect.TypeFor[prismBasic20](), pdfidtest.PrismNS20},
		{"prismBasic30", reflect.TypeFor[prismBasic30](), pdfidtest.PrismNS30},
	}

	var want []xmpModelField
	for _, m := range models {
		ns, prefix, fields := xmpModelFields(m.typ)

		if ns != m.ns {
			t.Errorf("%s: namespace = %q, want %q", m.name, ns, m.ns)
		}
		if prefix != "prism" {
			t.Errorf("%s: prefix = %q, want %q", m.name, prefix, "prism")
		}
		for _, f := range fields {
			if f.tag == "" {
				t.Errorf("%s: field %s has no xmp tag, so go-xmp would look up the property as %q",
					m.name, f.name, f.name)
			}
		}

		if want == nil {
			want = fields
			continue
		}
		if !reflect.DeepEqual(fields, want) {
			t.Errorf("%s properties:\n got %v\nwant %v (as declared by %s)",
				m.name, fields, want, models[0].name)
		}
	}

	// Guard the guard: a model whose fields all stopped being read would
	// make the comparison above vacuously true.
	if len(want) != 13 {
		t.Errorf("found %d PRISM properties, want the 13 declared ones", len(want))
	}
}

// xmpModelField is one property of an XMP model: the Go field name and
// the xmp struct tag that names the property in the packet.
type xmpModelField struct {
	name string
	tag  string
}

func (f xmpModelField) String() string { return f.name + `:"` + f.tag + `"` }

// xmpModelFields reports the namespace URI and preferred prefix declared
// by an XMP model's marker fields, and the property fields in
// declaration order with their xmp tags.  It mirrors how go-xmp itself
// reads a model (see Packet.Get): the markers are recognised by type,
// and a property is any other field whose type implements xmp.Value.
func xmpModelFields(typ reflect.Type) (ns, prefix string, fields []xmpModelField) {
	nsType := reflect.TypeFor[xmp.Namespace]()
	prefixType := reflect.TypeFor[xmp.Prefix]()
	valueType := reflect.TypeFor[xmp.Value]()

	for i := range typ.NumField() {
		f := typ.Field(i)
		switch {
		case f.Type == nsType:
			ns = f.Tag.Get("xmp")
		case f.Type == prefixType:
			prefix = f.Tag.Get("xmp")
		case f.Type.Implements(valueType):
			fields = append(fields, xmpModelField{name: f.Name, tag: f.Tag.Get("xmp")})
		}
	}
	return ns, prefix, fields
}

// TestPrismModelRoundTrip checks that the typed PRISM model can be
// written into a packet and read back unchanged.  This is the basic
// check that the marker-field pattern (_ xmp.Namespace / _ xmp.Prefix)
// is declared correctly: a wrong namespace tag or a misspelled property
// tag would still round-trip, but a malformed model would not decode at
// all.
func TestPrismModelRoundTrip(t *testing.T) {
	want := prismBasic21{
		DOI:             xmp.NewText("10.1016/j.jcp.2010.01.002"),
		PublicationName: xmp.NewText("Journal of Computational Physics"),
		ISSN:            xmp.NewText("0021-9991"),
		EISSN:           xmp.NewText("1090-2716"),
		Volume:          xmp.NewText("229"),
		Number:          xmp.NewText("9"),
		IssueIdentifier: xmp.NewText("9"),
		StartingPage:    xmp.NewText("3448"),
		EndingPage:      xmp.NewText("3468"),
		PageRange:       xmp.NewText("3448-3468"),
		CoverDate:       xmp.NewText("2010-05-01"),
		AggregationType: xmp.NewText("journal"),
		URL:             xmp.NewText("https://example.org/article"),
	}

	p := xmp.NewPacket()
	if err := p.Set(&want); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got prismBasic21
	if err := p.Get(&got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	// xmp.Text carries a qualifier slice and so is not comparable;
	// compare the text of each property instead.
	gotV, wantV := prismValues(got), prismValues(want)
	for _, f := range []struct {
		name      string
		got, want xmp.Text
	}{
		{"doi", gotV.DOI, wantV.DOI},
		{"publicationName", gotV.PublicationName, wantV.PublicationName},
		{"issn", gotV.ISSN, wantV.ISSN},
		{"eIssn", gotV.EISSN, wantV.EISSN},
		{"volume", gotV.Volume, wantV.Volume},
		{"number", gotV.Number, wantV.Number},
		{"issueIdentifier", gotV.IssueIdentifier, wantV.IssueIdentifier},
		{"startingPage", gotV.StartingPage, wantV.StartingPage},
		{"endingPage", gotV.EndingPage, wantV.EndingPage},
		{"pageRange", gotV.PageRange, wantV.PageRange},
		{"coverDate", gotV.CoverDate, wantV.CoverDate},
		{"aggregationType", gotV.AggregationType, wantV.AggregationType},
		{"url", gotV.URL, wantV.URL},
	} {
		if f.got.V != f.want.V {
			t.Errorf("prism:%s round trip = %q, want %q", f.name, f.got.V, f.want.V)
		}
	}

	// The properties must really live in the PRISM 2.1 namespace under
	// their PRISM names, not under the Go field names.
	if _, err := xmp.PacketGetValue[xmp.Text](p, pdfidtest.PrismNS21, "publicationName"); err != nil {
		t.Errorf("prism:publicationName not stored in the 2.1 namespace: %v", err)
	}
}

// TestPrismSICIDOIExact is the headline property of the typed path: a
// legacy SICI DOI, which contains '<' and '>', comes back byte for byte.
// The regex scan over DocText.XMP truncates such a DOI at the first '<'
// (see TestIdentifyTier1XMPSICIDOI); reading prism:doi as a typed value
// never involves a regex, so nothing is lost.
func TestPrismSICIDOIExact(t *testing.T) {
	const full = `10.1002/(SICI)1097-0258(19960229)15:4<361::AID-SIM168>3.0.CO;2-4`

	path := filepath.Join(t.TempDir(), "sici-prism.pdf")
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS21, map[string]string{"doi": full})
	pdfidtest.MakePDF(t, path, "", "", []string{"body text without any identifier"}, nil,
		pdfidtest.WithXMP(packet))

	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Prism == nil {
		t.Fatal("Prism = nil, want the PRISM metadata")
	}
	if d.Prism.DOI != full {
		t.Errorf("Prism.DOI = %q, want the exact SICI DOI %q", d.Prism.DOI, full)
	}

	id := Identify(d, Config{})
	if id.Tier != 1 || id.DOI != full {
		t.Errorf("id = %+v, want the exact SICI DOI at tier 1", id)
	}
}

// TestPrismDOINormalization pins the proxy-URL rule.  PRISM 2.0 defined
// prism:doi as carrying a DOI proxy URL rather than the DOI string, and
// producers followed both readings, so all of these forms occur in the
// same field.  Every one must reduce to the bare DOI; a value that is
// not a DOI at all must yield no DOI rather than a garbage one, since an
// unnormalized value would be sent to Crossref's works endpoint verbatim
// and 404.
func TestPrismDOINormalization(t *testing.T) {
	const sici = `10.1002/(SICI)1097-0258(19960229)15:4<361::AID-SIM168>3.0.CO;2-4`

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"bare", "10.1234/x", "10.1234/x"},
		{"doi prefix", "doi:10.1234/x", "10.1234/x"},
		{"uppercase prefix", "DOI:10.1234/x", "10.1234/x"},
		{"https proxy", "https://doi.org/10.1234/x", "10.1234/x"},
		{"dx proxy", "http://dx.doi.org/10.1234/x", "10.1234/x"},
		{"sici via proxy", "https://doi.org/" + sici, sici},
		{"bare issn", "0021-9991", ""},
		{"unrelated url", "https://example.org/article/12345", ""},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS21,
				map[string]string{"doi": tc.value})
			info := readPrism(packet)

			var got string
			if info != nil {
				got = info.DOI
			}
			if got != tc.want {
				t.Errorf("prism:doi %q -> %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestPrismNamespaceVersions checks that the 2.0 and 3.0 PRISM Basic
// namespaces are read by the same code path as 2.1.  The namespace URI
// is part of the struct tag, so each version needs its own model; this
// test is what keeps the three in step.
func TestPrismNamespaceVersions(t *testing.T) {
	for _, ns := range []string{pdfidtest.PrismNS20, pdfidtest.PrismNS21, pdfidtest.PrismNS30} {
		t.Run(ns, func(t *testing.T) {
			packet := pdfidtest.PrismPacket(t, ns, map[string]string{
				"doi":             "10.1234/x",
				"publicationName": "Journal of Testing",
				"volume":          "12",
			})
			info := readPrism(packet)
			if info == nil {
				t.Fatal("readPrism = nil, want the PRISM metadata")
			}
			if info.DOI != "10.1234/x" {
				t.Errorf("DOI = %q, want 10.1234/x", info.DOI)
			}
			if info.PublicationName != "Journal of Testing" {
				t.Errorf("PublicationName = %q, want Journal of Testing", info.PublicationName)
			}
			if info.Volume != "12" {
				t.Errorf("Volume = %q, want 12", info.Volume)
			}
		})
	}
}

// TestPrismBibliographicFields checks the merged, version-independent
// field set against a packet shaped like a real Elsevier article, which
// gives startingPage/endingPage rather than pageRange and eIssn rather
// than issn.
func TestPrismBibliographicFields(t *testing.T) {
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS20, map[string]string{
		"doi":             "10.1016/j.jcp.2010.01.002",
		"publicationName": "Journal of Computational Physics",
		"eIssn":           "1090-2716",
		"volume":          "229",
		"number":          "9",
		"startingPage":    "3448",
		"endingPage":      "3468",
		"coverDate":       "2010-05-01",
		"aggregationType": "journal",
	})

	info := readPrism(packet)
	if info == nil {
		t.Fatal("readPrism = nil")
	}
	want := PrismInfo{
		DOI:             "10.1016/j.jcp.2010.01.002",
		PublicationName: "Journal of Computational Physics",
		ISSN:            "1090-2716",
		Volume:          "229",
		Number:          "9",
		Pages:           "3448--3468",
		CoverDate:       "2010-05-01",
		AggregationType: "journal",
	}
	if *info != want {
		t.Errorf("readPrism = %+v, want %+v", *info, want)
	}
}

// TestPrismPageRangeWins checks that prism:pageRange is preferred over
// the startingPage/endingPage pair when both are present.
func TestPrismPageRangeWins(t *testing.T) {
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS21, map[string]string{
		"pageRange":    "361-372",
		"startingPage": "361",
		"endingPage":   "999",
	})
	info := readPrism(packet)
	if info == nil {
		t.Fatal("readPrism = nil")
	}
	if info.Pages != "361-372" {
		t.Errorf("Pages = %q, want the pageRange value 361-372", info.Pages)
	}
}

// TestPDFXDOIFallback checks that pdfx:doi is used when prism:doi is
// absent.  Springer and others put the article's DOI there.
func TestPDFXDOIFallback(t *testing.T) {
	packet := xmp.NewPacket()
	pdfidtest.SetProperties(t, packet, pdfidtest.PDFXNS,
		map[string]string{"doi": "https://doi.org/10.1007/s00440-009-0230-x"})

	info := readPrism(packet)
	if info == nil {
		t.Fatal("readPrism = nil, want the pdfx DOI")
	}
	if info.DOI != "10.1007/s00440-009-0230-x" {
		t.Errorf("DOI = %q, want the normalized pdfx:doi", info.DOI)
	}
}

// TestPrismDOIWinsOverPDFX checks the documented preference order:
// prism:doi first, pdfx:doi only as a fallback.
func TestPrismDOIWinsOverPDFX(t *testing.T) {
	packet := pdfidtest.PrismPacket(t, pdfidtest.PrismNS21,
		map[string]string{"doi": "10.1234/prism"})
	pdfidtest.SetProperties(t, packet, pdfidtest.PDFXNS,
		map[string]string{"doi": "10.1234/pdfx"})

	info := readPrism(packet)
	if info == nil || info.DOI != "10.1234/prism" {
		t.Errorf("readPrism = %+v, want the prism:doi value", info)
	}
}

// TestPrismSurvivesMalformedProperty applies the Packet.Get rule: Get
// populates every property it can decode and returns the per-property
// errors joined, so a single malformed property must not discard the
// rest.  A prism:volume written as an rdf:Seq (instead of a simple text
// value) is the malformed property here.
func TestPrismSurvivesMalformedProperty(t *testing.T) {
	const body = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:prism="http://prismstandard.org/namespaces/basic/2.1/">` +
		`<prism:doi>10.1234/x</prism:doi>` +
		`<prism:publicationName>Journal of Testing</prism:publicationName>` +
		`<prism:volume><rdf:Seq><rdf:li>12</rdf:li></rdf:Seq></prism:volume>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>` +
		`<?xpacket end="r"?>`

	p, err := xmp.Read(strings.NewReader(body))
	if err != nil {
		t.Fatalf("xmp.Read: %v", err)
	}

	// sanity check: confirm the premise that Get reports an error here.
	var m prismBasic21
	if err := p.Get(&m); err == nil {
		t.Fatal("Get did not report an error for the malformed prism:volume; test premise is stale")
	}

	info := readPrism(p)
	if info == nil {
		t.Fatal("readPrism = nil, want the properties which did decode")
	}
	if info.DOI != "10.1234/x" {
		t.Errorf("DOI = %q, want 10.1234/x despite the malformed prism:volume", info.DOI)
	}
	if info.PublicationName != "Journal of Testing" {
		t.Errorf("PublicationName = %q, want it despite the malformed prism:volume", info.PublicationName)
	}
	if info.Volume != "" {
		t.Errorf("Volume = %q, want empty: the property is malformed", info.Volume)
	}
}

// TestPrismAbsent checks that a packet with no PRISM or pdfx property
// leaves DocText.Prism nil, so that documents without publisher metadata
// behave exactly as they did before this path existed.
func TestPrismAbsent(t *testing.T) {
	packet := pdfidtest.DublinCorePacket(t, "doi:10.1214/aop/1176996548", "A title")
	if info := readPrism(packet); info != nil {
		t.Errorf("readPrism = %+v, want nil for a packet without PRISM data", info)
	}

	path := filepath.Join(t.TempDir(), "plain.pdf")
	pdfidtest.MakePDF(t, path, "Some title", "", []string{"body text"}, nil)
	d, err := Extract(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Prism != nil {
		t.Errorf("Prism = %+v, want nil for a PDF without a metadata stream", d.Prism)
	}
	if d.XMPDOI != "" {
		t.Errorf("XMPDOI = %q, want empty for a PDF without a metadata stream", d.XMPDOI)
	}
}

// TestXMPDOIFromDublinCore checks that dc:identifier is normalized on
// the typed path too: it also carries a DOI proxy URL in the wild.  It
// is the last resort, after prism:doi and pdfx:doi, and it does not by
// itself make a document count as carrying PRISM metadata.
func TestXMPDOIFromDublinCore(t *testing.T) {
	packet := pdfidtest.DublinCorePacket(t, "http://dx.doi.org/10.1214/aop/1176996548", "")

	if got := xmpDOI(packet, nil); got != "10.1214/aop/1176996548" {
		t.Errorf("xmpDOI = %q, want the normalized dc:identifier DOI", got)
	}
	if info := readPrism(packet); info != nil {
		t.Errorf("readPrism = %+v, want nil: dc:identifier is not PRISM data", info)
	}
}

// TestIdentifyTier1PrismBeatsInfoDict checks that the typed prism:doi is
// preferred over a DOI-looking string in the Info dictionary.  The typed
// value is an assertion by the producer about which article this is; the
// Info-dict scan is a regex guess over free text.
func TestIdentifyTier1PrismBeatsInfoDict(t *testing.T) {
	d := &DocText{
		Subject: "see also 10.9999/wrong",
		Prism:   &PrismInfo{DOI: "10.1234/right"},
		XMPDOI:  "10.1234/right",
	}
	id := Identify(d, Config{})
	if id.Tier != 1 || id.DOI != "10.1234/right" {
		t.Errorf("id = %+v, want the typed prism:doi at tier 1", id)
	}
}
