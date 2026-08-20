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

package bibtex

import (
	"fmt"
	"sort"
)

// Entry represents a BibTeX entry with a type and fields.
type Entry struct {
	Type   string            // e.g., "article", "book", "inproceedings"
	Fields map[string]string // field names to values (already bibtex-encoded)
}

// FieldOrder defines the canonical order for bibtex fields.
var FieldOrder = []string{
	"author", "editor", "title", "journal", "booktitle", "series",
	"edition", "chapter", "type", "institution", "school", "publisher",
	"organization", "address", "howpublished", "volume", "number",
	"pages", "month", "year", "eprint", "archiveprefix", "primaryclass",
	"doi", "isbn", "url", "note",
}

// RequiredFields maps entry types to lists of required field groups.
// Each group is satisfied if ANY listed field is present.
// All groups must be satisfied for the entry to be valid.
var RequiredFields = map[string][][]string{
	"article":       {{"author"}, {"title"}, {"journal"}, {"year"}},
	"book":          {{"author", "editor"}, {"title"}, {"publisher"}, {"year"}},
	"incollection":  {{"author"}, {"title"}, {"booktitle"}, {"publisher"}, {"year"}},
	"inproceedings": {{"author"}, {"title"}, {"booktitle"}, {"year"}},
	"phdthesis":     {{"author"}, {"title"}, {"school"}, {"year"}},
	"mastersthesis": {{"author"}, {"title"}, {"school"}, {"year"}},
	"techreport":    {{"author"}, {"title"}, {"institution"}, {"year"}},
	"unpublished":   {{"author"}, {"title"}, {"note"}},
	"misc":          {},
}

// KnownTypes contains all known entry types.
var KnownTypes = func() []string {
	types := make([]string, 0, len(RequiredFields))
	for t := range RequiredFields {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}()

// Format serializes an entry to a deterministic BibTeX format.
// The output includes:
// - Entry type and key: @<type>{<key>,
// - Fields in order (defined by FieldOrder), with unknown fields alphabetically at the end
// - Two-space indentation, one field per line, trailing comma after each field
// - Closing brace with trailing newline
// Values are emitted verbatim (assumed to be already bibtex-encoded).
func Format(key string, e Entry) string {
	var result string
	result += fmt.Sprintf("@%s{%s,\n", e.Type, key)

	// Build list of fields to output in order
	fieldOrder := make([]string, 0, len(e.Fields))
	knownFieldsInOrder := make([]string, 0)
	unknownFields := make([]string, 0)

	// First pass: identify which fields are in FieldOrder and which are unknown
	for field := range e.Fields {
		found := false
		for _, ordered := range FieldOrder {
			if field == ordered {
				found = true
				break
			}
		}
		if found {
			knownFieldsInOrder = append(knownFieldsInOrder, field)
		} else {
			unknownFields = append(unknownFields, field)
		}
	}

	// Sort known fields by FieldOrder
	sort.Slice(knownFieldsInOrder, func(i, j int) bool {
		iIdx := -1
		jIdx := -1
		for k, f := range FieldOrder {
			if f == knownFieldsInOrder[i] {
				iIdx = k
			}
			if f == knownFieldsInOrder[j] {
				jIdx = k
			}
		}
		return iIdx < jIdx
	})

	// Sort unknown fields alphabetically
	sort.Strings(unknownFields)

	// Combine: known fields in order, then unknown fields alphabetically
	fieldOrder = append(knownFieldsInOrder, unknownFields...)

	// Output fields
	for _, field := range fieldOrder {
		value := e.Fields[field]
		result += fmt.Sprintf("  %s = {%s},\n", field, value)
	}

	result += "}\n"
	return result
}
