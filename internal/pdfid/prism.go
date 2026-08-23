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
	"strings"

	"seehuhn.de/go/xmp"

	"seehuhn.de/go/paper/internal/sources"
)

// This file reads publisher metadata out of a PDF's XMP packet using
// typed go-xmp models rather than the regex scan over DocText.XMP.
//
// Two things make the typed path worth having.  First, the fields:
// Elsevier, Wiley, Springer and SAGE embed PRISM metadata carrying the
// journal name, ISSN, volume, issue, page range and cover date - exactly
// the bibliographic fields a draft entry is otherwise missing.  Second,
// the fidelity: a typed value is read from the parsed packet, so it is
// never XML-entity-escaped and never passes through doiRegex.  A legacy
// SICI DOI, which contains '<' and '>', therefore comes back byte for
// byte, where the regex scan truncates it at the first '<'.

// PrismInfo is the merged, namespace-version-independent view of the
// publisher metadata found in a document's XMP packet.  A nil *PrismInfo
// means the packet carried no PRISM (or pdfx) property this package
// models; an individual field is "" when the packet did not carry it.
//
// The bibliographic fields are raw metadata: they are neither
// tex-encoded nor bibtex-normalized here.  That happens where they are
// consumed, in internal/resolve.
type PrismInfo struct {
	// DOI is the bare DOI of the article, normalized through
	// [sources.ParseRef]: prism:doi if present, otherwise pdfx:doi.  It
	// is "" when neither is present or when the value is not a DOI at
	// all.  See [normalizeDOI] for why the normalization is mandatory.
	DOI string

	// PublicationName is prism:publicationName, the journal or book title.
	PublicationName string

	// ISSN is prism:issn, falling back to prism:eIssn.
	ISSN string

	// Volume is prism:volume.
	Volume string

	// Number is the issue number: prism:number, falling back to
	// prism:issueIdentifier.
	Number string

	// Pages is the page range: prism:pageRange if present, otherwise
	// prism:startingPage and prism:endingPage joined with the bibtex
	// en-dash "--" (or prism:startingPage alone, which is what a journal
	// using article numbers supplies).
	Pages string

	// CoverDate is prism:coverDate, the issue's cover date.
	CoverDate string

	// AggregationType is prism:aggregationType, e.g. "journal" or "book".
	AggregationType string
}

// The four PRISM Basic models below are deliberate copies of one field
// set.  go-xmp takes the namespace URI from a struct tag, and a struct
// tag must be a literal, so the URI is fixed at compile time and one
// model cannot serve several namespace versions.  Publishers use 2.0,
// 2.1 and 3.0 interchangeably (Elsevier writes 2.0 to this day), so all
// three have to be declared; 1.2 joins them because it is the likeliest
// version on 1990s-era Wiley files, which is also where legacy SICI DOIs
// live (see [TestPrismSICIDOIExact]).
//
// Keep the field lists identical.  Two guards enforce that, and it takes
// both:
//
//   - The conversions to [prismValues] are struct conversions, so adding,
//     removing, renaming or retyping a field in one copy alone stops the
//     package building.
//   - They catch nothing else.  Go ignores struct tags when checking type
//     identity for a conversion, so misspelling a tag in one copy - say
//     `xmp:"eissn"` for `xmp:"eIssn"` in the 2.0 model - compiles
//     cleanly and silently yields an absent property for that version.
//     TestPrismModelTagsAgree compares the (field name, xmp tag) pairs of
//     the three models by reflection and is the only thing standing
//     between a typo there and data quietly going missing.

// prismBasic21 models the PRISM Basic 2.1 namespace.  The property names
// follow the PRISM specification; note the capital "I" in "eIssn".
type prismBasic21 struct {
	_ xmp.Namespace `xmp:"http://prismstandard.org/namespaces/basic/2.1/"`
	_ xmp.Prefix    `xmp:"prism"`

	DOI             xmp.Text `xmp:"doi"`
	PublicationName xmp.Text `xmp:"publicationName"`
	ISSN            xmp.Text `xmp:"issn"`
	EISSN           xmp.Text `xmp:"eIssn"`
	Volume          xmp.Text `xmp:"volume"`
	Number          xmp.Text `xmp:"number"`
	IssueIdentifier xmp.Text `xmp:"issueIdentifier"`
	StartingPage    xmp.Text `xmp:"startingPage"`
	EndingPage      xmp.Text `xmp:"endingPage"`
	PageRange       xmp.Text `xmp:"pageRange"`
	CoverDate       xmp.Text `xmp:"coverDate"`
	AggregationType xmp.Text `xmp:"aggregationType"`
	URL             xmp.Text `xmp:"url"`
}

// prismBasic20 models the PRISM Basic 2.0 namespace, the version
// Elsevier's article PDFs use.
type prismBasic20 struct {
	_ xmp.Namespace `xmp:"http://prismstandard.org/namespaces/basic/2.0/"`
	_ xmp.Prefix    `xmp:"prism"`

	DOI             xmp.Text `xmp:"doi"`
	PublicationName xmp.Text `xmp:"publicationName"`
	ISSN            xmp.Text `xmp:"issn"`
	EISSN           xmp.Text `xmp:"eIssn"`
	Volume          xmp.Text `xmp:"volume"`
	Number          xmp.Text `xmp:"number"`
	IssueIdentifier xmp.Text `xmp:"issueIdentifier"`
	StartingPage    xmp.Text `xmp:"startingPage"`
	EndingPage      xmp.Text `xmp:"endingPage"`
	PageRange       xmp.Text `xmp:"pageRange"`
	CoverDate       xmp.Text `xmp:"coverDate"`
	AggregationType xmp.Text `xmp:"aggregationType"`
	URL             xmp.Text `xmp:"url"`
}

// prismBasic30 models the PRISM Basic 3.0 namespace.
type prismBasic30 struct {
	_ xmp.Namespace `xmp:"http://prismstandard.org/namespaces/basic/3.0/"`
	_ xmp.Prefix    `xmp:"prism"`

	DOI             xmp.Text `xmp:"doi"`
	PublicationName xmp.Text `xmp:"publicationName"`
	ISSN            xmp.Text `xmp:"issn"`
	EISSN           xmp.Text `xmp:"eIssn"`
	Volume          xmp.Text `xmp:"volume"`
	Number          xmp.Text `xmp:"number"`
	IssueIdentifier xmp.Text `xmp:"issueIdentifier"`
	StartingPage    xmp.Text `xmp:"startingPage"`
	EndingPage      xmp.Text `xmp:"endingPage"`
	PageRange       xmp.Text `xmp:"pageRange"`
	CoverDate       xmp.Text `xmp:"coverDate"`
	AggregationType xmp.Text `xmp:"aggregationType"`
	URL             xmp.Text `xmp:"url"`
}

// prismBasic12 models the legacy PRISM Basic 1.2 namespace. This is the
// earliest PRISM version, and the likeliest one on 1990s-era Wiley PDFs
// - exactly the files that carry SICI DOIs with the unescaped '<' and
// '>' [TestPrismSICIDOIExact] pins.
//
// The 1.2 specification is not available to check its property names
// against; the tags below mirror prismBasic20's verbatim rather than
// risk inventing a namespace-specific spelling that silently decodes
// nothing.  If a 1.2 producer turns up using different property names,
// this is the model to correct.
type prismBasic12 struct {
	_ xmp.Namespace `xmp:"http://prismstandard.org/namespaces/1.2/basic/"`
	_ xmp.Prefix    `xmp:"prism"`

	DOI             xmp.Text `xmp:"doi"`
	PublicationName xmp.Text `xmp:"publicationName"`
	ISSN            xmp.Text `xmp:"issn"`
	EISSN           xmp.Text `xmp:"eIssn"`
	Volume          xmp.Text `xmp:"volume"`
	Number          xmp.Text `xmp:"number"`
	IssueIdentifier xmp.Text `xmp:"issueIdentifier"`
	StartingPage    xmp.Text `xmp:"startingPage"`
	EndingPage      xmp.Text `xmp:"endingPage"`
	PageRange       xmp.Text `xmp:"pageRange"`
	CoverDate       xmp.Text `xmp:"coverDate"`
	AggregationType xmp.Text `xmp:"aggregationType"`
	URL             xmp.Text `xmp:"url"`
}

// prismValues is the namespace-independent carrier for the values read
// from any of the PRISM Basic models.  It has the same field sequence as
// the models but no tags, so the three models convert to it directly (Go
// ignores struct tags when checking type identity for a conversion).
//
// Its untagged marker field is a runtime tripwire, not a compile-time
// impossibility: passing a prismValues to Packet.Get compiles, and Get
// then panics with "not an XMP namespace struct" because it finds no
// namespace URI.  That is the intended outcome - a decode into this type
// is a mistake - but it surfaces when the code runs, not when it builds.
type prismValues struct {
	_ xmp.Namespace
	_ xmp.Prefix

	DOI             xmp.Text
	PublicationName xmp.Text
	ISSN            xmp.Text
	EISSN           xmp.Text
	Volume          xmp.Text
	Number          xmp.Text
	IssueIdentifier xmp.Text
	StartingPage    xmp.Text
	EndingPage      xmp.Text
	PageRange       xmp.Text
	CoverDate       xmp.Text
	AggregationType xmp.Text
	URL             xmp.Text
}

// pdfxCustom models the DOI property of the Adobe "custom document
// properties" namespace, where Springer and others put the article's
// DOI.
//
// go-xmp's own xmp.PDFX model claims the same namespace URI for the
// legacy PDF/X identification properties (GTS_PDFXVersion and friends).
// That is not a conflict to resolve but a fact about the wild: producers
// use http://ns.adobe.com/pdfx/1.3/ for both purposes, so a second model
// over the same URI is exactly what is needed.
type pdfxCustom struct {
	_ xmp.Namespace `xmp:"http://ns.adobe.com/pdfx/1.3/"`
	_ xmp.Prefix    `xmp:"pdfx"`

	DOI xmp.Text `xmp:"doi"`
}

// prismReaders decodes a packet with each PRISM Basic model in turn. The
// order is the precedence [readPrismBasic] merges them in: 2.1 (the
// version the specification settled on), then 2.0 (still the most common
// in article PDFs), then 3.0, then 1.2 (the legacy version, least likely
// to appear alongside the others). Precedence matters only when two
// versions both carry the same field; see [readPrismBasic].
var prismReaders = []func(*xmp.Packet) prismValues{
	func(p *xmp.Packet) prismValues { var m prismBasic21; _ = p.Get(&m); return prismValues(m) },
	func(p *xmp.Packet) prismValues { var m prismBasic20; _ = p.Get(&m); return prismValues(m) },
	func(p *xmp.Packet) prismValues { var m prismBasic30; _ = p.Get(&m); return prismValues(m) },
	func(p *xmp.Packet) prismValues { var m prismBasic12; _ = p.Get(&m); return prismValues(m) },
}

// readPrism returns the publisher metadata of an XMP packet, or nil when
// the packet carries none of the modelled properties.
func readPrism(p *xmp.Packet) *PrismInfo {
	var info PrismInfo
	if v, ok := readPrismBasic(p); ok {
		info = PrismInfo{
			DOI:             normalizeDOI(v.DOI.V),
			PublicationName: trimXMP(v.PublicationName),
			ISSN:            firstNonEmpty(trimXMP(v.ISSN), trimXMP(v.EISSN)),
			Volume:          trimXMP(v.Volume),
			Number:          firstNonEmpty(trimXMP(v.Number), trimXMP(v.IssueIdentifier)),
			Pages:           prismPages(v),
			CoverDate:       trimXMP(v.CoverDate),
			AggregationType: trimXMP(v.AggregationType),
		}
	}

	if info.DOI == "" {
		// Packet.Get's error is deliberately ignored: it populates every
		// property it can decode and only zeroes the ones that failed,
		// joining the per-property errors.  A malformed property
		// elsewhere in the namespace must not discard a good pdfx:doi.
		var px pdfxCustom
		_ = p.Get(&px)
		info.DOI = normalizeDOI(px.DOI.V)
	}

	if info == (PrismInfo{}) {
		return nil
	}
	return &info
}

// readPrismBasic decodes the PRISM Basic properties of a packet, trying
// every namespace version and merging the results field by field: for
// each field, the first version in [prismReaders]'s precedence order to
// carry a non-empty value wins. A document that states PRISM 2.1 with a
// DOI but no pages, and PRISM 3.0 with pages but no DOI, therefore yields
// both, rather than whichever namespace happens to match first supplying
// every field (including the ones it left empty) on its own. ok is false
// when no version yielded anything.
//
// As in [readPrism], the error from Packet.Get is ignored on purpose: a
// packet with one malformed PRISM property must still give up the rest.
func readPrismBasic(p *xmp.Packet) (prismValues, bool) {
	var merged prismValues
	found := false
	for _, read := range prismReaders {
		v := read(p)
		if v.isZero() {
			continue
		}
		found = true
		merged = merged.mergeFrom(v)
	}
	return merged, found
}

// mergeFrom fills every field of v that is empty from the corresponding
// field of other, leaving a field v already carries untouched. It is the
// per-field counterpart of [firstNonEmpty], applied at the typed
// xmp.Text stage that [readPrismBasic] merges at, before [trimXMP]
// reduces each field to a plain string.
func (v prismValues) mergeFrom(other prismValues) prismValues {
	v.DOI = firstNonEmptyText(v.DOI, other.DOI)
	v.PublicationName = firstNonEmptyText(v.PublicationName, other.PublicationName)
	v.ISSN = firstNonEmptyText(v.ISSN, other.ISSN)
	v.EISSN = firstNonEmptyText(v.EISSN, other.EISSN)
	v.Volume = firstNonEmptyText(v.Volume, other.Volume)
	v.Number = firstNonEmptyText(v.Number, other.Number)
	v.IssueIdentifier = firstNonEmptyText(v.IssueIdentifier, other.IssueIdentifier)
	v.StartingPage = firstNonEmptyText(v.StartingPage, other.StartingPage)
	v.EndingPage = firstNonEmptyText(v.EndingPage, other.EndingPage)
	v.PageRange = firstNonEmptyText(v.PageRange, other.PageRange)
	v.CoverDate = firstNonEmptyText(v.CoverDate, other.CoverDate)
	v.AggregationType = firstNonEmptyText(v.AggregationType, other.AggregationType)
	v.URL = firstNonEmptyText(v.URL, other.URL)
	return v
}

// firstNonEmptyText returns a if it carries a value, otherwise b. Unlike
// [firstNonEmpty], it compares typed xmp.Text values via IsZero rather
// than plain strings, since [mergeFrom] runs before [trimXMP] applies.
func firstNonEmptyText(a, b xmp.Text) xmp.Text {
	if !a.IsZero() {
		return a
	}
	return b
}

// isZero reports whether none of the properties that reach [PrismInfo]
// was present.  prism:url is excluded deliberately: it is modelled for
// completeness but not surfaced, so a namespace version carrying only a
// URL must not stop [readPrismBasic] from trying the next version.
func (v prismValues) isZero() bool {
	return v.DOI.IsZero() && v.PublicationName.IsZero() &&
		v.ISSN.IsZero() && v.EISSN.IsZero() &&
		v.Volume.IsZero() && v.Number.IsZero() && v.IssueIdentifier.IsZero() &&
		v.StartingPage.IsZero() && v.EndingPage.IsZero() && v.PageRange.IsZero() &&
		v.CoverDate.IsZero() && v.AggregationType.IsZero()
}

// prismPages derives a bibtex-shaped page range from the PRISM page
// properties: prism:pageRange when the producer supplied one, otherwise
// the startingPage/endingPage pair.  A starting page without an ending
// page is kept on its own, since that is how journals which number
// articles rather than pages fill the field.
func prismPages(v prismValues) string {
	if r := trimXMP(v.PageRange); r != "" {
		return r
	}
	start, end := trimXMP(v.StartingPage), trimXMP(v.EndingPage)
	if start != "" && end != "" {
		return start + "--" + end
	}
	return start
}

// xmpDOI returns the DOI the XMP packet states, read as a typed value
// and normalized: PrismInfo.DOI (prism:doi, else pdfx:doi) if there is
// one, otherwise dc:identifier.  It is "" when the packet names no DOI.
//
// dc:identifier is included because it carries a DOI proxy URL in the
// wild just as prism:doi does, and normalizing it here is what lets
// tier 1 use it verbatim.  It is deliberately not part of [PrismInfo]:
// a Dublin Core identifier is not publisher metadata, and a document
// carrying only dc:identifier must still report Prism == nil.
//
// As elsewhere in this package, the error from Packet.Get is ignored:
// the populated struct is used even when some other property in the
// namespace failed to decode.
func xmpDOI(p *xmp.Packet, info *PrismInfo) string {
	if info != nil && info.DOI != "" {
		return info.DOI
	}
	var dc xmp.DublinCore
	_ = p.Get(&dc)
	return normalizeDOI(dc.Identifier.V)
}

// normalizeDOI reduces a DOI-bearing metadata value to the bare DOI,
// returning "" when the value is not a DOI at all.
//
// This is not optional tidying.  PRISM 2.0 specified prism:doi as
// carrying a DOI *proxy URL* rather than the DOI string; the field was
// later corrected, but producers followed both readings, so
// "10.1234/x", "doi:10.1234/x", "https://doi.org/10.1234/x" and
// "http://dx.doi.org/10.1234/x" all occur in the same field.  dc:identifier
// and pdfx:doi have the same problem.  A proxy URL left unnormalized
// would be handed to Crossref's works endpoint verbatim, 404, and turn a
// perfectly identified paper into an unidentified one.
//
// The work is done by [sources.ParseRef], which already strips these
// prefixes case-insensitively and validates what is left against the DOI
// syntax; a non-DOI value comes back with an empty Ref.DOI, which is the
// correct rejection.  Reusing it is deliberate: a fourth hand-written
// prefix stripper is a fourth place to get this wrong.
func normalizeDOI(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return sources.ParseRef(s).DOI
}

// trimXMP returns the text of an XMP value with surrounding whitespace
// removed; producers pad these fields with newlines and indentation.
func trimXMP(t xmp.Text) string {
	return strings.TrimSpace(t.V)
}

// firstNonEmpty returns the first of its arguments which is not "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
