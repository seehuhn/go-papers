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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/sources"
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

// firstSurname returns the bibtex-encoded surname of the first author, or
// "" when the author field is missing or does not parse.
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
// may legitimately carry `note` or `url`. Fields are visited in
// bibtex.FieldOrder, then any remaining store field names sorted
// alphabetically, matching bibtex.Format's convention — never in map
// order, which varies from run to run.
func diffAgainstStore(e bibtex.KeyedEntry, p *store.Paper) []string {
	var out []string
	for _, name := range storeFieldOrder(p.Bibtex.Fields) {
		storeVal := p.Bibtex.Fields[name]
		if strings.TrimSpace(storeVal) == "" {
			continue
		}
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

// storeFieldOrder returns the names in fields in a deterministic order:
// bibtex.FieldOrder's fields first (in that order), then any remaining
// field names sorted alphabetically — the same convention bibtex.Format
// uses for known-vs-unknown fields.
func storeFieldOrder(fields map[string]string) []string {
	seen := make(map[string]bool, len(fields))
	var out []string
	for _, name := range bibtex.FieldOrder {
		if _, ok := fields[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range fields {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// foldedJoin folds s into its tokens (see match.Tokens) and rejoins them
// with a single space, so that punctuation and whitespace differences do
// not register as disagreements while real wording differences still do.
func foldedJoin(s string) string {
	return strings.Join(match.Tokens(s), " ")
}

// verify resolves a reference against the online sources.
//
// An identifier that resolves is proof. Everything else is a search, and a
// search result is only ever a candidate: it is accepted as proof of
// existence when the title clears titleBar AND something else agrees, and
// otherwise it is handed to the agent with its score. Only a search that
// found nothing at all supports the likely-hallucinated claim — a search
// where every source errored proves nothing either way and comes back
// "unchecked", never "notFound".
func (a *auditor) verify(e bibtex.KeyedEntry) (string, []auditCandidate) {
	if doi := strings.TrimSpace(e.Entry.Fields["doi"]); doi != "" {
		switch _, err := a.crossref().Work(doi); {
		case err == nil:
			return "confirmed", nil
		case !errors.Is(err, sources.ErrNotFound):
			// The source is down, not the paper missing. Saying "not found"
			// here would accuse a real paper of being invented.
			return "unchecked", nil
		}
	}
	if id := arxivIDOf(e.Entry); id != "" {
		switch _, err := a.arxiv().ByID(id); {
		case err == nil:
			return "confirmed", nil
		case !errors.Is(err, sources.ErrNotFound):
			return "unchecked", nil
		}
	}

	cands, ok := a.search(e)
	if !ok {
		// Every source that could have answered errored out; we have not
		// established that nothing exists anywhere.
		return "unchecked", nil
	}
	if len(cands) == 0 {
		return "notFound", nil
	}
	best := cands[0]
	if best.Score >= titleBar && corroboratesCandidate(e.Entry, best.Candidate) {
		return "confirmed", nil
	}
	if len(cands) > 3 {
		cands = cands[:3]
	}
	out := make([]auditCandidate, len(cands))
	for i, c := range cands {
		out[i] = auditCandidate{
			Score:   c.Score,
			Title:   c.Title,
			Authors: strings.Join(c.Authors, ", "),
			Year:    c.Year,
			DOI:     c.DOI,
			Source:  c.Source,
		}
	}
	return "unverified", out
}

// scoredCandidate pairs a search hit with its title-similarity score
// against the bib entry being verified.
type scoredCandidate struct {
	sources.Candidate
	Score float64
}

// search queries the candidate sources — Crossref, zbMATH and DBLP — with
// the bib title plus the first author's surname, scores every hit with
// match.TitleSimilarity against the bib title, and returns them sorted by
// descending score. A source that errors is skipped, not fatal: a partial
// answer beats none. ok is false only when every source errored, meaning
// the search established nothing about whether the reference exists.
func (a *auditor) search(e bibtex.KeyedEntry) (cands []scoredCandidate, ok bool) {
	title := e.Entry.Fields["title"]
	query := strings.TrimSpace(title + " " + firstSurname(e.Entry))

	add := func(hits []sources.Candidate) {
		for _, c := range hits {
			cands = append(cands, scoredCandidate{c, match.TitleSimilarity(title, c.Title)})
		}
	}

	if hits, err := a.crossref().Search(query, searchRows); err == nil {
		ok = true
		for _, w := range hits {
			c := crossrefCandidate(w)
			cands = append(cands, scoredCandidate{c, match.TitleSimilarity(title, c.Title)})
		}
	}
	if hits, err := a.zbmath().Search(query, searchRows); err == nil {
		ok = true
		add(hits)
	}
	if hits, err := a.dblp().Search(query, searchRows); err == nil {
		ok = true
		add(hits)
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	return cands, ok
}

// corroboratesCandidate mirrors corroborates but reads a search
// candidate's Authors and Year rather than a bibtex.Entry's. A candidate's
// first author may render as "Family, Given" or "Given Family"; matching
// on a fold-insensitive substring match of the bib entry's first-author
// surname against that whole string is robust to either order without
// needing to parse it.
func corroboratesCandidate(e bibtex.Entry, c sources.Candidate) bool {
	if ey := e.Fields["year"]; ey != "" && c.Year != 0 {
		if n, err := strconv.Atoi(ey); err == nil && abs(n-c.Year) <= 1 {
			return true
		}
	}
	surname := firstSurname(e)
	if surname == "" || len(c.Authors) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(c.Authors[0]), strings.ToLower(surname))
}

// crossref, arxiv, zbmath and dblp return clients for this run, following
// the constructor pattern in cmd_fetch.go.
func (a *auditor) crossref() *sources.Crossref {
	return &sources.Crossref{BaseURL: crossrefBase, Client: a.api, Email: a.email}
}

func (a *auditor) arxiv() *sources.Arxiv {
	return &sources.Arxiv{BaseURL: arxivBase, Client: a.api}
}

func (a *auditor) zbmath() *sources.ZbMath {
	return &sources.ZbMath{BaseURL: zbmathBase, Client: a.api}
}

func (a *auditor) dblp() *sources.DBLP {
	return &sources.DBLP{BaseURL: dblpBase, Client: a.api}
}
