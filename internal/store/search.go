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

import (
	"sort"
	"strings"

	"seehuhn.de/go/paper/internal/tex"
)

// Hit is one paper matched by Search, together with its relevance score.
type Hit struct {
	Paper *Paper
	Score float64
}

// searchFields holds, for one paper, the pre-normalized text of every
// field Search matches against. It is computed once per paper per query
// so that repeated per-term matching does not redo tex.Fold/ToLower work.
type searchFields struct {
	key      string // tex.Fold(Key)
	doi      string // strings.ToLower(DOI)
	arxiv    string // strings.ToLower(Arxiv.ID)
	author   string // tex.Fold(Fields["author"])
	title    string // tex.Fold(Fields["title"])
	year     string // tex.Fold(Fields["year"])
	journal  string // tex.Fold(Fields["journal"] + " " + Fields["booktitle"])
	abstract string // tex.Fold(Abstract)
}

// fieldWeight is the score awarded for a search term matching one field,
// per the weights given in the task-9 brief.
const (
	weightKey    = 5
	weightID     = 5 // DOI or arXiv ID
	weightAuthor = 3
	weightTitle  = 2
	weightYear   = 2
	weightVenue  = 1 // journal/booktitle
	weightAbs    = 1
)

// buildSearchFields precomputes the normalized field text for one paper.
func buildSearchFields(p *Paper) searchFields {
	arxivID := ""
	if p.Arxiv != nil {
		arxivID = p.Arxiv.ID
	}
	venue := strings.TrimSpace(p.Bibtex.Fields["journal"] + " " + p.Bibtex.Fields["booktitle"])
	return searchFields{
		key:      tex.Fold(p.Key),
		doi:      strings.ToLower(p.DOI),
		arxiv:    strings.ToLower(arxivID),
		author:   tex.Fold(p.Bibtex.Fields["author"]),
		title:    tex.Fold(p.Bibtex.Fields["title"]),
		year:     tex.Fold(p.Bibtex.Fields["year"]),
		journal:  tex.Fold(venue),
		abstract: tex.Fold(p.Abstract),
	}
}

// termWeight returns the best (highest) field weight with which term
// matches f, and whether term matched at all. folded is tex.Fold(term);
// lower is strings.ToLower(term), used for the verbatim DOI/arXiv match.
func termWeight(f searchFields, folded, lower string) (float64, bool) {
	best := 0.0
	found := false
	consider := func(weight float64, matched bool) {
		if matched && weight > best {
			best = weight
			found = true
		}
	}

	if folded != "" {
		consider(weightKey, strings.Contains(f.key, folded))
		consider(weightAuthor, strings.Contains(f.author, folded))
		consider(weightTitle, strings.Contains(f.title, folded))
		consider(weightYear, f.year == folded)
		consider(weightVenue, strings.Contains(f.journal, folded))
		consider(weightAbs, strings.Contains(f.abstract, folded))
	}
	if lower != "" {
		consider(weightID, strings.Contains(f.doi, lower) || strings.Contains(f.arxiv, lower))
	}

	return best, found
}

// Search finds every paper matching all of terms (AND semantics across
// terms) and returns them as Hits, sorted by descending score and then
// ascending key. A term matches a paper if it matches (as a
// tex.Fold-ed, or for DOI/arXiv IDs verbatim-lowercase, substring - see
// termWeight) any of the paper's key, DOI, arXiv ID, author, title,
// year, journal/booktitle, or abstract fields; a paper's score is the
// sum, over all terms, of the best field weight that term matched.
func (s *Store) Search(terms []string) ([]Hit, error) {
	papers, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	return searchPapers(papers, terms), nil
}

// searchPapers implements the matching and ranking described in Search's
// doc comment, for an already-loaded slice of papers.
func searchPapers(papers []*Paper, terms []string) []Hit {
	folded := make([]string, len(terms))
	lowered := make([]string, len(terms))
	for i, term := range terms {
		folded[i] = tex.Fold(term)
		lowered[i] = strings.ToLower(term)
	}

	var hits []Hit
	for _, p := range papers {
		fields := buildSearchFields(p)
		total := 0.0
		matched := true
		for i := range terms {
			w, ok := termWeight(fields, folded[i], lowered[i])
			if !ok {
				matched = false
				break
			}
			total += w
		}
		if matched {
			hits = append(hits, Hit{Paper: p, Score: total})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Paper.Key < hits[j].Paper.Key
	})

	return hits
}
