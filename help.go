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

package main

// schemaHelp documents the store layout and the paper.json format for
// "paper help schema". This text is the authority the librarian skills
// defer to: when the layout or the schema changes, change this text in
// the same commit.
const schemaHelp = `The store layout and the paper.json format.

A store is a plain directory tree, one subdirectory per paper - no
database, no index, everything inspectable with ls and a text editor:

    <store>/
    |-- .paper-store.json        {"paperStore": 1}, marks the directory
    |-- events/<hostname>.jsonl  local, append-only log of command outcomes
    ` + "`" + `-- <key>/
        |-- paper.json               the entry: single source of truth
        |-- published.pdf            version of record, when held
        |-- arxiv-2412.05039v2.pdf   arXiv PDF, version-qualified name
        ` + "`" + `-- arxiv-2412.05039v2/      extracted arXiv tex source

<key> is surname_year of the first author, lowercase ASCII, underscore
before the year (hoeffding_1963), disambiguated with a/b suffixes
(crisan_1999a). The key is simultaneously the directory name and the
bibtex key; "paper check" enforces that they match. Old-style arXiv
IDs keep their slash in paper.json but use a dash in file names.

paper.json is one JSON object. It is loaded with unknown-member
rejection: a misspelled or misplaced field breaks loading of the whole
entry, not just that field. Fields:

    key       entry key, matching the directory name
    status    "draft" (metadata not yet trusted; see pending) or
              "clean" (has passed "paper check")
    pending   task left for an agent while the entry is a draft
    bibtex    {"type": ..., "fields": {...}}: bibtex field names
              mapped to values (see the encoding rule below)
    doi       DOI
    arxiv     {"id": ..., "version": ...}
    isbn      ISBN
    leeds     University of Leeds Primo catalogue permalink
    abstract  plain text
    holdings  which versions are held: "none", "preprint",
              "published", or "both"
    versions  map from held filename to {"acquired": ISO date,
              "source": ..., "deprecated": bool, "note": ...}
    audit     {"claims": [...]}, see below
    log       acquisition and correction history: append, never rewrite

The bibtex-encoding rule: values inside bibtex.fields are stored
already bibtex-encoded, exactly as they will be exported - LaTeX
escapes and all:

    "author": "Vo{\\ss}, Jochen"
    "title":  "A study of {SPDEs} in {G}reenland"
    "pages":  "13--30"

Because this is JSON, every backslash in a value must be DOUBLED when
typed by hand, as in the author above. Follow any hand edit with
"paper check <key>", which catches the usual mistakes immediately.

Semantic-verification claims (recorded by bibliography audits) live
under audit.claims - nested inside "audit", never at top level:

    "audit": {
      "claims": [
        {
          "claim":   "The citing sentence, verbatim.",
          "source":  "chapter3.tex:142",
          "verdict": "supports",
          "version": "published.pdf",
          "date":    "2026-08-23",
          "note":    "optional"
        }
      ]
    }

claim, verdict, version and date are required; source and note are
optional. verdict is one of supports, partial, refutes, unverifiable;
version must name a file the entry currently holds; date is an ISO
date. "paper check" validates all of this.

The event log events/<hostname>.jsonl holds one JSON line per command
outcome: what ran, how it ended, which source resolved it. It is
append-only, per-machine, jq-able, and read by no command; nothing in
it ever leaves the machine.

The store location comes from the -store flag when given, and from the
config file otherwise (~/.paper.json, or the file named by
$PAPER_CONFIG); see "paper help init".
`

func init() {
	helpTopics = append(helpTopics, helpTopic{
		name: "schema",
		desc: "the store layout and the paper.json format",
		help: schemaHelp,
	})
}
