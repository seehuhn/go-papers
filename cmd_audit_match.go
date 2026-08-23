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

import (
	"fmt"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/store"
)

// titleBar is the similarity a title match must clear. It is deliberately
// higher than the 0.8 the pile ingest used: 5 shared tokens out of 6 scores
// 0.833, which is how Giles's paper was filed as Giles & Waterhouse's.
const titleBar = 0.9

// matchStore finds the store entry a bib entry refers to, by DOI, then
// arXiv ID, then title. Returns nil when the store holds no match.
func matchStore(papers []*store.Paper, e bibtex.KeyedEntry) *store.Paper {
	doi := strings.TrimSpace(e.Entry.Fields["doi"])
	if doi != "" {
		for _, p := range papers {
			if p.DOI != "" && strings.EqualFold(p.DOI, doi) {
				return p
			}
		}
	}
	if id := arxivIDOf(e.Entry); id != "" {
		for _, p := range papers {
			if p.Arxiv != nil && p.Arxiv.ID == id {
				return p
			}
		}
	}
	title := e.Entry.Fields["title"]
	if title == "" {
		return nil
	}
	for _, p := range papers {
		if match.TitleSimilarity(title, p.Bibtex.Fields["title"]) >= titleBar &&
			corroborates(e.Entry, p.Bibtex) {
			return p
		}
	}
	return nil
}

// corroborates reports whether two entries agree on something beyond their
// titles — the first author's surname, or the year within one. A title
// alone is never enough to call two references the same work.
func corroborates(a, b bibtex.Entry) bool {
	if ya, yb := a.Fields["year"], b.Fields["year"]; ya != "" && yb != "" {
		na, erra := strconv.Atoi(ya)
		nb, errb := strconv.Atoi(yb)
		if erra == nil && errb == nil && abs(na-nb) <= 1 {
			return true
		}
	}
	sa, sb := firstSurname(a), firstSurname(b)
	return sa != "" && strings.EqualFold(sa, sb)
}

// firstSurname returns the folded surname of the first author, or "" when
// the author field is missing or does not parse.
func firstSurname(e bibtex.Entry) string {
	names, err := bibtex.ParseNames(e.Fields["author"])
	if err != nil || len(names) == 0 {
		return ""
	}
	return names[0].Last
}

// arxivIDOf returns the arXiv identifier an entry carries, from the
// standard `eprint` field, or "" when it has none.
func arxivIDOf(e bibtex.Entry) string {
	id := strings.TrimSpace(e.Fields["eprint"])
	return strings.TrimPrefix(strings.ToLower(id), "arxiv:")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// diffAgainstStore reports where the bib entry disagrees with the store
// entry, which is the authority for anything the store holds. A field the
// bib entry has and the store lacks is never reported — the bibliography
// may legitimately carry `note` or `url`.
func diffAgainstStore(e bibtex.KeyedEntry, p *store.Paper) []string {
	var out []string
	for name, storeVal := range p.Bibtex.Fields {
		bibVal, ok := e.Entry.Fields[name]
		if !ok || strings.TrimSpace(bibVal) == "" {
			out = append(out, fmt.Sprintf("missing field %q (store has %q)", name, storeVal))
			continue
		}
		if foldedJoin(bibVal) != foldedJoin(storeVal) {
			out = append(out, fmt.Sprintf("%s is %q, store has %q", name, bibVal, storeVal))
		}
	}
	if p.DOI != "" && strings.TrimSpace(e.Entry.Fields["doi"]) == "" {
		out = append(out, fmt.Sprintf("missing field %q (store has %q)", "doi", p.DOI))
	}
	return out
}

// foldedJoin folds s into its tokens (see match.Tokens) and rejoins them
// with a single space, so that punctuation and whitespace differences do
// not register as disagreements while real wording differences still do.
func foldedJoin(s string) string {
	return strings.Join(match.Tokens(s), " ")
}
