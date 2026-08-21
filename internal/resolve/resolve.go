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

// Package resolve maps already-fetched source records (Crossref works,
// arXiv entries) onto draft store.Paper entries with bibtex-encoded
// fields. It is pure: no network access and no store I/O. The pipeline
// that calls the source clients and writes the result to the store lives
// in the fetch command.
package resolve

import (
	"fmt"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/pdfid"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
	"seehuhn.de/go/paper/internal/tex"
)

// pendingMsg is the fixed Pending text left on every draft produced by
// this package, directing a later agent to polish the bibtex fields.
const pendingMsg = "polish bibtex: verify fields against the paper, brace-protect proper nouns ({G}reenland style), check non-ASCII encoding"

// Download is one file the fetch command should try to acquire.
type Download struct {
	URL      string
	Filename string // "published.pdf" or "arxiv-<id>v<n>.pdf"
	Source   string // "unpaywall" | "arxiv"
	IsSource bool   // true for the arXiv e-print tarball (extracted, not attached)
}

// crossrefTypeMap maps Crossref work types to bibtex entry types; any
// type not listed here maps to "misc".
var crossrefTypeMap = map[string]string{
	"journal-article":     "article",
	"book":                "book",
	"monograph":           "book",
	"book-chapter":        "incollection",
	"proceedings-article": "inproceedings",
}

// crossrefType returns the bibtex entry type for a Crossref work type.
func crossrefType(t string) string {
	if bt, ok := crossrefTypeMap[t]; ok {
		return bt
	}
	return "misc"
}

// FromCrossref builds the draft bibtex fields for a published work.
func FromCrossref(w *sources.CrossrefWork) (*store.Paper, error) {
	identity := crossrefIdentity(w)

	if len(w.Titles) == 0 || w.Titles[0] == "" {
		return nil, fmt.Errorf("crossref work %s: missing title", identity)
	}
	if len(w.Authors) == 0 {
		return nil, fmt.Errorf("crossref work %s: missing authors", identity)
	}
	year := w.Published.Year()
	if year == 0 {
		return nil, fmt.Errorf("crossref work %s: missing year", identity)
	}

	authorField, err := encodeCrossrefAuthors(w.Authors)
	if err != nil {
		return nil, fmt.Errorf("crossref work %s: %w", identity, err)
	}
	yearStr := strconv.Itoa(year)

	key, err := store.MakeKey(authorField, yearStr)
	if err != nil {
		return nil, fmt.Errorf("crossref work %s: %w", identity, err)
	}

	fields := map[string]string{
		"author": authorField,
		"title":  bibtex.BraceTitle(tex.Encode(w.Titles[0])),
		"year":   yearStr,
	}
	if len(w.ContainerTitle) > 0 && w.ContainerTitle[0] != "" {
		fields["journal"] = tex.Encode(w.ContainerTitle[0])
	}
	if w.Volume != "" {
		fields["volume"] = w.Volume
	}
	if w.Issue != "" {
		fields["number"] = w.Issue
	}
	if w.Page != "" {
		fields["pages"] = bibtex.NormalizePages(w.Page)
	}
	if w.DOI != "" {
		fields["doi"] = w.DOI
	}

	return &store.Paper{
		Key:      key,
		Status:   "draft",
		Pending:  pendingMsg,
		Holdings: "none",
		DOI:      w.DOI,
		Bibtex: bibtex.Entry{
			Type:   crossrefType(w.Type),
			Fields: fields,
		},
	}, nil
}

// FillFromPrism fills the gaps in an already-resolved draft entry from
// the PRISM metadata a publisher embedded in the PDF: journal, volume,
// number, pages and issn. It never overwrites a field that is already
// set, so the Crossref record stays authoritative wherever it answered;
// PRISM only supplies what Crossref left out. A nil entry or nil info
// does nothing, so the caller need not check either.
//
// This is a separate function rather than an extra parameter of
// [FromCrossref] on purpose: the PRISM data comes from the file being
// ingested, not from the source record, and only the ingest command
// holds both. Threading it through FromCrossref would change a signature
// three commands and a dozen tests depend on to carry a value two of
// those callers never have.
//
// The values are treated exactly like the Crossref ones: the journal
// name is tex.Encode'd and the page range goes through
// [bibtex.NormalizePages].
func FillFromPrism(p *store.Paper, info *pdfid.PrismInfo) {
	if p == nil || info == nil {
		return
	}
	if p.Bibtex.Fields == nil {
		p.Bibtex.Fields = make(map[string]string)
	}

	fillGap(p.Bibtex.Fields, "journal", tex.Encode(info.PublicationName))
	fillGap(p.Bibtex.Fields, "volume", info.Volume)
	fillGap(p.Bibtex.Fields, "number", info.Number)
	fillGap(p.Bibtex.Fields, "pages", bibtex.NormalizePages(info.Pages))
	fillGap(p.Bibtex.Fields, "issn", info.ISSN)
}

// fillGap sets fields[name] to value, unless the field already has a
// value or the new one is empty.
func fillGap(fields map[string]string, name, value string) {
	if value == "" || fields[name] != "" {
		return
	}
	fields[name] = value
}

// FromArxiv builds a preprint-only draft (@misc with eprint fields). The
// eprint field carries the bare arXiv ID, with no version suffix: that
// matches arXiv's own bibtex snippets and store.CheckPaper's
// eprint/arxiv.id consistency rule. The version is recorded in
// Arxiv.Version, and in the version-qualified names of the downloaded
// files.
func FromArxiv(e *sources.ArxivEntry) (*store.Paper, error) {
	identity := arxivIdentity(e)

	if e.Title == "" {
		return nil, fmt.Errorf("arxiv entry %s: missing title", identity)
	}
	if len(e.Authors) == 0 {
		return nil, fmt.Errorf("arxiv entry %s: missing authors", identity)
	}
	if e.Year == 0 {
		return nil, fmt.Errorf("arxiv entry %s: missing year", identity)
	}

	authorField, err := encodeArxivAuthors(e.Authors)
	if err != nil {
		return nil, fmt.Errorf("arxiv entry %s: %w", identity, err)
	}
	yearStr := strconv.Itoa(e.Year)

	key, err := store.MakeKey(authorField, yearStr)
	if err != nil {
		return nil, fmt.Errorf("arxiv entry %s: %w", identity, err)
	}

	fields := map[string]string{
		"author":        authorField,
		"title":         bibtex.BraceTitle(tex.Encode(e.Title)),
		"year":          yearStr,
		"eprint":        e.ID,
		"archiveprefix": "arXiv",
	}
	if e.PrimaryClass != "" {
		fields["primaryclass"] = e.PrimaryClass
	}

	return &store.Paper{
		Key:      key,
		Status:   "draft",
		Pending:  pendingMsg,
		Holdings: "none",
		Abstract: e.Abstract,
		Arxiv:    &store.ArxivRef{ID: e.ID, Version: e.Version},
		Bibtex: bibtex.Entry{
			Type:   "misc",
			Fields: fields,
		},
	}, nil
}

// Merge overlays arXiv supplementary fields (eprint, archiveprefix,
// primaryclass, abstract, arxiv ref) onto a Crossref-derived paper: the
// published metadata is the entry, per the spec's best-version rule. As
// in FromArxiv, the eprint field is the bare ID.
func Merge(published *store.Paper, e *sources.ArxivEntry) *store.Paper {
	m := *published

	fields := make(map[string]string, len(published.Bibtex.Fields)+3)
	for k, v := range published.Bibtex.Fields {
		fields[k] = v
	}
	fields["eprint"] = e.ID
	fields["archiveprefix"] = "arXiv"
	if e.PrimaryClass != "" {
		fields["primaryclass"] = e.PrimaryClass
	}
	m.Bibtex = bibtex.Entry{Type: published.Bibtex.Type, Fields: fields}

	m.Arxiv = &store.ArxivRef{ID: e.ID, Version: e.Version}
	m.Abstract = e.Abstract

	return &m
}

// encodeCrossrefAuthors builds a bibtex author field from Crossref author
// records, tex.Encoding each name. Crossref author arrays can contain
// collective/organization entries (e.g. {"name": "LIGO Scientific
// Collaboration"}) that carry a literal Name with Family and Given both
// absent; these render as a braced bibtex literal name, which
// bibtex.ParseNames treats as a single opaque token (Tame-the-BeaST
// rules), so the collaboration name is never split into first/von/last
// parts. An entry with Name/Family/Given all empty is skipped rather than
// rejected - Crossref data occasionally has these - unless every entry in
// the list is empty, in which case the usual "no authors" error applies.
func encodeCrossrefAuthors(authors []sources.CrossrefAuthor) (string, error) {
	if len(authors) == 0 {
		return "", fmt.Errorf("no authors")
	}
	var parts []string
	for _, a := range authors {
		switch {
		case a.Family == "" && a.Given == "" && a.Name == "":
			// No usable data at all: skip this entry.
			continue
		case a.Family == "" && a.Name != "":
			// Organization/collective author: a literal name, braced so
			// bibtex.ParseNames keeps it as one token instead of
			// splitting on internal whitespace.
			parts = append(parts, "{"+tex.Encode(a.Name)+"}")
		default:
			family := tex.Encode(a.Family)
			given := tex.Encode(a.Given)
			if given == "" {
				parts = append(parts, family)
			} else {
				parts = append(parts, family+", "+given)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no authors")
	}
	return strings.Join(parts, " and "), nil
}

// encodeArxivAuthors builds a bibtex author field from arXiv's
// natural-order plain-unicode names ("Jochen Voß"), splitting each on the
// last space into given/family (crude but deterministic; repair is a
// later, human-in-the-loop task) and tex.Encoding the result.
func encodeArxivAuthors(names []string) (string, error) {
	if len(names) == 0 {
		return "", fmt.Errorf("no authors")
	}
	parts := make([]string, len(names))
	for i, n := range names {
		family, given := splitArxivName(n)
		family = tex.Encode(family)
		given = tex.Encode(given)
		if given == "" {
			parts[i] = family
		} else {
			parts[i] = family + ", " + given
		}
	}
	return strings.Join(parts, " and "), nil
}

// splitArxivName splits a natural-order plain-unicode name on its last
// space into family and given parts. A name with no space is taken to be
// entirely a family name.
func splitArxivName(name string) (family, given string) {
	i := strings.LastIndex(name, " ")
	if i < 0 {
		return name, ""
	}
	return name[i+1:], name[:i]
}

// crossrefIdentity names a Crossref work for use in error messages, per
// the agent-fallback contract: prefer the DOI, falling back to a
// placeholder when it too is absent.
func crossrefIdentity(w *sources.CrossrefWork) string {
	if w.DOI != "" {
		return w.DOI
	}
	return "(no DOI)"
}

// arxivIdentity names an arXiv entry for use in error messages, per the
// agent-fallback contract: prefer the ID, falling back to a placeholder
// when it too is absent.
func arxivIdentity(e *sources.ArxivEntry) string {
	if e.ID != "" {
		return e.ID
	}
	return "(no arXiv ID)"
}
