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

package sources

import (
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want Ref
	}{
		{"10.1080/01621459.1963.10500830",
			Ref{Kind: RefDOI, DOI: "10.1080/01621459.1963.10500830"}},
		{"https://doi.org/10.1007/BF00535479",
			Ref{Kind: RefDOI, DOI: "10.1007/BF00535479"}},
		{"doi:10.1007/BF00535479",
			Ref{Kind: RefDOI, DOI: "10.1007/BF00535479"}},
		{"2412.05039", Ref{Kind: RefArxiv, ArxivID: "2412.05039"}},
		{"2412.05039v2", Ref{Kind: RefArxiv, ArxivID: "2412.05039", Version: 2}},
		{"arXiv:2412.05039v2", Ref{Kind: RefArxiv, ArxivID: "2412.05039", Version: 2}},
		{"https://arxiv.org/abs/2412.05039v2",
			Ref{Kind: RefArxiv, ArxivID: "2412.05039", Version: 2}},
		{"https://arxiv.org/pdf/2412.05039", Ref{Kind: RefArxiv, ArxivID: "2412.05039"}},
		{"math.PR/0605234", Ref{Kind: RefArxiv, ArxivID: "math.PR/0605234"}},
		{"Hoeffding, Probability inequalities, 1963",
			Ref{Kind: RefText, Text: "Hoeffding, Probability inequalities, 1963"}},
		{"  10.1214/aop/1176996548  ", Ref{Kind: RefDOI, DOI: "10.1214/aop/1176996548"}},
		{"DOI:10.1007/BF00535479", Ref{Kind: RefDOI, DOI: "10.1007/BF00535479"}},
		{"arxiv:2412.05039v2", Ref{Kind: RefArxiv, ArxivID: "2412.05039", Version: 2}},
		{"ARXIV:2412.05039", Ref{Kind: RefArxiv, ArxivID: "2412.05039"}},

		// A URL that neither the DOI nor the arXiv extractor claims is a
		// candidate PDF: the download verifies the %PDF- magic, so the
		// extension is not what decides this.
		{"https://users.aalto.fi/~ssarkka/pub/cup_book_online_20131111.pdf",
			Ref{Kind: RefPDFURL,
				URL: "https://users.aalto.fi/~ssarkka/pub/cup_book_online_20131111.pdf"}},
		{"http://example.org/papers/draft.pdf",
			Ref{Kind: RefPDFURL, URL: "http://example.org/papers/draft.pdf"}},
		{"https://example.org/download?id=42",
			Ref{Kind: RefPDFURL, URL: "https://example.org/download?id=42"}},
		{"  https://example.org/a.pdf  ",
			Ref{Kind: RefPDFURL, URL: "https://example.org/a.pdf"}},
		// A doi.org or arxiv.org URL whose identifier does not parse is
		// still just a URL; nothing better is known about it.
		{"https://doi.org/not-a-doi",
			Ref{Kind: RefPDFURL, URL: "https://doi.org/not-a-doi"}},
	}
	for _, c := range cases {
		if got := ParseRef(c.in); got != c.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
