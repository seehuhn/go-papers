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

func bibtexEntryStub() bibtex.Entry {
	return bibtex.Entry{Type: "misc", Fields: map[string]string{}}
}

func TestMakeKey(t *testing.T) {
	cases := []struct{ author, year, want string }{
		{"Hoeffding, Wassily", "1963", "hoeffding_1963"},
		{`Vo{\ss}, Jochen`, "2004", "voss_2004"},
		{`L\'evy, Paul`, "1937", "levy_1937"},
		{"Smith-Jones, Ann and Doe, Jane", "2020", "smith-jones_2020"},
		{"de la Vall{\\'e}e Poussin, Charles", "1896", "vallee-poussin_1896"},
	}
	for _, c := range cases {
		got, err := MakeKey(c.author, c.year)
		if err != nil {
			t.Errorf("MakeKey(%q): %v", c.author, err)
			continue
		}
		if got != c.want {
			t.Errorf("MakeKey(%q, %q) = %q, want %q", c.author, c.year, got, c.want)
		}
	}
	if _, err := MakeKey("", "1963"); err == nil {
		t.Error("expected error for empty author")
	}
	if _, err := MakeKey("{ }, X", "1963"); err == nil {
		t.Error("expected error for degenerate author field with no usable letters")
	}
}

func TestFreeKey(t *testing.T) {
	s := testStore(t)
	// no existing entry: base itself
	k, _ := s.FreeKey("hoeffding_1963")
	if k != "hoeffding_1963" {
		t.Errorf("FreeKey = %q", k)
	}
	// existing entry: first suffix
	p := &Paper{Key: "hoeffding_1963", Status: "draft", Holdings: "none",
		Bibtex: bibtexEntryStub()}
	s.Save(p)
	k, _ = s.FreeKey("hoeffding_1963")
	if k != "hoeffding_1963a" {
		t.Errorf("FreeKey = %q, want hoeffding_1963a", k)
	}
}
