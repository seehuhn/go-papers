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

package store

import "seehuhn.de/go/paper/internal/bibtex"

// ExportBibtex returns a copy of p.Bibtex with the "doi" and "isbn"
// fields set from the top-level DOI and ISBN, which are the authority
// (see checkConsistency and checkPromoteIdentifiers in check.go). A
// top-level field that is empty leaves the corresponding bibtex field
// untouched. The returned Entry's Fields map is a fresh copy, so callers
// may modify it without affecting p.Bibtex.Fields.
func (p *Paper) ExportBibtex() bibtex.Entry {
	fields := make(map[string]string, len(p.Bibtex.Fields))
	for k, v := range p.Bibtex.Fields {
		fields[k] = v
	}
	if p.DOI != "" {
		fields["doi"] = p.DOI
	}
	if p.ISBN != "" {
		fields["isbn"] = p.ISBN
	}
	return bibtex.Entry{Type: p.Bibtex.Type, Fields: fields}
}
