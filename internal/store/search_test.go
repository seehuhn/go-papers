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
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
)

// bibtexEntry builds a bibtex.Entry of the given type with the given
// fields, for use in test fixtures.
func bibtexEntry(typ string, fields map[string]string) bibtex.Entry {
	return bibtex.Entry{Type: typ, Fields: fields}
}

func searchFixture(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	s.Save(&Paper{Key: "hoeffding_1963", Status: "clean", Holdings: "published",
		Bibtex: bibtexEntry("article", map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963"})})
	s.Save(&Paper{Key: "voss_2004", Status: "clean", Holdings: "none",
		Bibtex: bibtexEntry("phdthesis", map[string]string{
			"author": `Vo{\ss}, Jochen`,
			"title":  "Some large deviation results for diffusion processes",
			"school": "Universit{\"a}t Kaiserslautern",
			"year":   "2004"})})
	return s
}

func TestSearchByFoldedAuthor(t *testing.T) {
	s := searchFixture(t)
	hits, err := s.Search([]string{"voss"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Paper.Key != "voss_2004" {
		t.Errorf("hits = %v", hits)
	}
}

func TestSearchAndSemantics(t *testing.T) {
	s := searchFixture(t)
	hits, _ := s.Search([]string{"hoeffding", "diffusion"})
	if len(hits) != 0 {
		t.Errorf("AND semantics violated: %v", hits)
	}
}

func TestSearchRanking(t *testing.T) {
	s := searchFixture(t)
	hits, _ := s.Search([]string{"1963"})
	if len(hits) != 1 || hits[0].Paper.Key != "hoeffding_1963" {
		t.Errorf("hits = %v", hits)
	}
}
