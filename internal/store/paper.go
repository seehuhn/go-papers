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

// Package store manages the on-disk store of papers: one directory per
// paper under a store root, each holding a paper.json that is the single
// source of truth for that paper.
package store

import "seehuhn.de/go/paper/internal/bibtex"

// Paper represents the full state of one paper in the store, as recorded
// in its paper.json file.
type Paper struct {
	Key      string             `json:"key"`
	Status   string             `json:"status"`           // "draft" | "clean"
	Pending  string             `json:"pending,omitzero"` // task left for an agent
	Bibtex   bibtex.Entry       `json:"bibtex"`
	DOI      string             `json:"doi,omitzero"`
	Arxiv    *ArxivRef          `json:"arxiv,omitzero"`
	ISBN     string             `json:"isbn,omitzero"`
	Leeds    string             `json:"leeds,omitzero"`    // Primo permalink
	Abstract string             `json:"abstract,omitzero"` // plain text
	Holdings string             `json:"holdings"`          // none|preprint|published|both
	Versions map[string]Version `json:"versions,omitzero"` // per held file
	Audit    *Audit             `json:"audit,omitzero"`
	Log      []LogEntry         `json:"log,omitzero"`
}

// Audit holds the semantic-verification records for one paper: which
// claims citing it have been checked, against which held version.
type Audit struct {
	Claims []Claim `json:"claims,omitzero"`
}

// Claim is one verified citation.
type Claim struct {
	Claim   string `json:"claim"`           // the citing sentence, verbatim
	Source  string `json:"source,omitzero"` // where it cites from
	Verdict string `json:"verdict"`         // supports|partial|refutes|unverifiable
	Version string `json:"version"`         // the held file that was read
	Date    string `json:"date"`            // ISO date
	Note    string `json:"note,omitzero"`
}

// ArxivRef identifies a paper's arXiv record.
type ArxivRef struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitzero"`
}

// Version records the provenance of one held file for a paper.
type Version struct {
	Acquired   string `json:"acquired,omitzero"` // ISO date
	Source     string `json:"source,omitzero"`
	Deprecated bool   `json:"deprecated,omitzero"`
	Note       string `json:"note,omitzero"`
}

// LogEntry records one entry in a paper's audit log.
type LogEntry struct {
	When   string `json:"when"` // ISO date-time
	Action string `json:"action"`
	Detail string `json:"detail,omitzero"`
}
