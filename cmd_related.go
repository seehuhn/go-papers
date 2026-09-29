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
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/tex"
)

const relatedHelp = `usage: paper related [options] <refs.bib>

Find works that a bibliography does not yet cite but that sit close to
it in the citation graph, according to OpenAlex. The store is only read,
never changed (apart from the event log).

Each entry of <refs.bib> is looked up in OpenAlex by its DOI (from the
doi field or from a doi.org URL in the url field), else by its arXiv ID,
else by its title; a title counts only if the best hit matches it with
similarity 0.9 or more. Then, over the entries found, two counts are
made for every other work:

    cited-by-yours <n>   how many of your works list it as a reference
    cites-yours <n>      how many of your works it cites

The second count looks only at the 200 newest citations of each of your
works, so an old, much cited work can miss some of them. Works are
ranked by the larger of the two counts, then by their sum, then by
citation count. Your own works and other versions of them (same DOI,
same arXiv ID or a title that matches at 0.9) are left out.

Plain output starts with the lines

    resolved: <found> of <entries> entries
    unresolved: <key>, <key>, ...

(the second only if some entry was not found), then

    total: <listed>, shown: <listed>

and one line per work, best first:

    <id> | <year> | <surname> | <title> | <venue> | cited <n> | cited-by-yours <n> | cites-yours <n> | doi:<doi> | arxiv:<id> | held:<key>

The cited-by-yours, cites-yours, doi, arxiv and held fields appear only
when they apply; held:<key> names the store paper that already holds the
work. Unless -short is given, each line is followed by an indented
"abstract: ..." line ("abstract: none" if OpenAlex has none).

With -json the output is one object {"total": n, "resolved": n,
"unresolved": [keys], "works": [...]}; each work has the fields id, doi,
arxiv, year, authors, title, venue, type, cited_by_count, abstract,
held, cited_by_yours and cites_yours. Only doi, arxiv, venue, abstract,
held, cited_by_yours and cites_yours are left out when empty or zero;
-short also drops abstract.

Entries that are not found are recorded in the event log, as are found
articles for which OpenAlex lists no references.

options:
    -n <count>    list at most <count> works (default 40)
    -since <year> only works published in <year> or later
    -short        leave out the abstracts
    -json         print the result as JSON
    -store <dir>  path to the paper store (overrides the configured store)

Anonymous OpenAlex requests are rate limited; a free key stored with
"paper init -openalex-key KEY <store dir>" removes the limit in practice.
`

func init() {
	commands = append(commands, command{
		name: "related",
		desc: "find works close to a bibliography in the citation graph",
		help: relatedHelp,
		run:  runRelated,
	})
}

// relatedCitingLimit is how many of the newest citations of each work
// are counted.
const relatedCitingLimit = 200

var arxivVersionSuffix = regexp.MustCompile(`v\d+$`)

// normArxiv folds an arXiv ID for comparison: lower case, no "arXiv:"
// prefix, no version suffix, and for an old-style ID no subject class
// ("math.PR/0611001" and "math/0611001" name the same paper).
func normArxiv(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimPrefix(id, "arxiv:")
	id = arxivVersionSuffix.ReplaceAllString(id, "")
	return sources.StripArxivClass(id)
}

// bibIdentity holds what identifies the works of a bibliography, in the
// folded spelling of normArxiv and strings.ToLower.
type bibIdentity struct {
	dois, arxivs map[string]bool
	titles       []string
}

// isCited reports whether w is a work the bibliography cites, in this or
// another version.
func (b *bibIdentity) isCited(w *sources.OpenAlexWork) bool {
	if w.DOI != "" && b.dois[strings.ToLower(w.DOI)] {
		return true
	}
	if w.ArxivID != "" && b.arxivs[normArxiv(w.ArxivID)] {
		return true
	}
	for _, t := range b.titles {
		if match.TitleSimilarity(w.Title, t) >= titleBar {
			return true
		}
	}
	return false
}

// relatedEntry is one bibliography entry: its key, and the identifiers
// and title it gives, decoded.
type relatedEntry struct {
	key, doi, arxiv, title string
}

func newRelatedEntry(e bibtex.KeyedEntry) relatedEntry {
	r := relatedEntry{key: e.Key}
	field := func(name string) string {
		v, _ := tex.Decode(e.Entry.Fields[name])
		return strings.TrimSpace(v)
	}
	// A DOI is the doi field, else one found in the url field; either
	// may be given as a doi.org URL.
	for _, name := range []string{"doi", "url"} {
		if ref := sources.ParseRef(field(name)); ref.Kind == sources.RefDOI {
			r.doi = ref.DOI
			break
		}
	}
	if ref := sources.ParseRef(arxivIDOf(e.Entry)); ref.Kind == sources.RefArxiv {
		r.arxiv = ref.ArxivID // OpenAlex.Work drops the subject class
	}
	r.title = field("title")
	return r
}

// ref names the entry in the event log: its key and its best identifier.
func (r relatedEntry) ref() string {
	switch {
	case r.doi != "":
		return r.key + " " + r.doi
	case r.arxiv != "":
		return r.key + " " + r.arxiv
	}
	return r.key + " " + r.title
}

// resolveEntry finds the OpenAlex work for a bibliography entry, or nil
// if OpenAlex does not have it.
func resolveEntry(oa *sources.OpenAlex, r relatedEntry) (*sources.OpenAlexWork, error) {
	for _, id := range []string{r.doi, r.arxiv} {
		if id == "" {
			continue
		}
		w, err := oa.Work(id)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, sources.ErrNotFound) {
			return nil, err
		}
	}
	if r.title == "" {
		return nil, nil
	}
	hits, err := oa.ByTitle(r.title)
	if err != nil {
		return nil, err
	}
	var best *sources.OpenAlexWork
	bestSim := 0.0
	for i := range hits {
		if sim := match.TitleSimilarity(hits[i].Title, r.title); best == nil || sim > bestSim {
			best, bestSim = &hits[i], sim
		}
	}
	if best == nil || bestSim < titleBar {
		return nil, nil
	}
	return best, nil
}

// relatedCand is a work that may be proposed, with the counts that rank it.
type relatedCand struct {
	id           string
	citedByYours int
	citesYours   int
	work         *sources.OpenAlexWork // nil until known
}

func (c *relatedCand) strength() (top, sum int) {
	return max(c.citedByYours, c.citesYours), c.citedByYours + c.citesYours
}

// runRelated implements "paper related": the works closest to a
// bibliography in the citation graph, which the bibliography does not cite.
func runRelated(args []string) error {
	fs, storeFlag := newFlagSet("related")
	n := fs.Int("n", 40, "list at most this many works")
	since := fs.Int("since", 0, "only works published in this year or later")
	short := fs.Bool("short", false, "leave out the abstracts")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("related: parsing arguments: %w", err)
	}
	if err := checkNoTrailingFlags("related", ".bib file", fs.Args()); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("related: expected exactly one .bib file, got %d arguments", fs.NArg())
	}
	if *n < 1 {
		return fmt.Errorf("related: -n must be at least 1, got %d", *n)
	}
	path := fs.Arg(0)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("related: %w", err)
	}
	defer f.Close()
	keyed, parseErrs := bibtex.Parse(f)
	for _, pe := range parseErrs {
		if pe.Line == 0 {
			return fmt.Errorf("related: %s: %s", path, pe.Msg)
		}
		fmt.Fprintf(os.Stderr, "related: %s:%d: %s\n", path, pe.Line, pe.Msg)
	}

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("related: %w", err)
	}

	start := time.Now()
	fail := func(err error) error {
		logOpenAlexErr(s, "related", path, err, time.Since(start))
		return fmt.Errorf("related: %w", err)
	}
	oa := newOpenAlex(cfg)

	// Resolve the entries.
	ident := &bibIdentity{dois: map[string]bool{}, arxivs: map[string]bool{}}
	resolved := map[string]*sources.OpenAlexWork{} // by ID
	var resolvedIDs []string
	var unresolved []string
	nResolved := 0
	for _, ke := range keyed {
		e := newRelatedEntry(ke)
		if e.doi != "" {
			ident.dois[strings.ToLower(e.doi)] = true
		}
		if e.arxiv != "" {
			ident.arxivs[normArxiv(e.arxiv)] = true
		}
		if e.title != "" {
			ident.titles = append(ident.titles, e.title)
		}

		w, err := resolveEntry(oa, e)
		if err != nil {
			return fail(err)
		}
		if w == nil {
			unresolved = append(unresolved, e.key)
			logOpenAlex(s, "related", e.ref(), "openalex-unresolved", 0, time.Since(start))
			continue
		}
		nResolved++
		if resolved[w.ID] == nil {
			resolved[w.ID] = w
			resolvedIDs = append(resolvedIDs, w.ID)
		}
		if ke.Entry.Type == "article" && len(w.References) == 0 {
			logOpenAlex(s, "related", e.key+" "+w.ID, "openalex-no-refs", 0, time.Since(start))
		}
	}

	// Count the links to and from the resolved works.
	cands := map[string]*relatedCand{}
	cand := func(id string) *relatedCand {
		c := cands[id]
		if c == nil {
			c = &relatedCand{id: id}
			cands[id] = c
		}
		return c
	}
	for _, id := range resolvedIDs {
		for _, ref := range slices.Compact(slices.Sorted(slices.Values(resolved[id].References))) {
			cand(ref).citedByYours++
		}
		citing, _, err := oa.Citing(id, relatedCitingLimit, *since)
		if err != nil {
			return fail(err)
		}
		seen := map[string]bool{}
		for i := range citing {
			c := cand(citing[i].ID)
			if seen[c.id] {
				continue
			}
			seen[c.id] = true
			c.citesYours++
			c.work = &citing[i]
		}
	}
	for id := range resolved {
		delete(cands, id)
	}

	// Rank in tiers of equal strength. Within a tier the order depends
	// on the citation counts, so the whole tier is fetched before it is
	// sorted, dropping cited works and applying -since on the way.
	ranked := make([]*relatedCand, 0, len(cands))
	for _, c := range cands {
		ranked = append(ranked, c)
	}
	slices.SortFunc(ranked, func(a, b *relatedCand) int {
		at, as := a.strength()
		bt, bs := b.strength()
		switch {
		case at != bt:
			return bt - at
		case as != bs:
			return bs - as
		}
		return strings.Compare(a.id, b.id)
	})

	var picked []workLine
	for i := 0; i < len(ranked) && len(picked) < *n; {
		j := i + 1
		for ti, ts := ranked[i].strength(); j < len(ranked); j++ {
			if tj, sj := ranked[j].strength(); tj != ti || sj != ts {
				break
			}
		}
		tier := ranked[i:j]
		i = j

		var missing []string
		for _, c := range tier {
			if c.work == nil {
				missing = append(missing, c.id)
			}
		}
		if len(missing) > 0 {
			ws, err := oa.Works(missing)
			if err != nil {
				return fail(err)
			}
			byID := map[string]*sources.OpenAlexWork{}
			for k := range ws {
				byID[ws[k].ID] = &ws[k]
			}
			for _, c := range tier {
				if c.work == nil {
					c.work = byID[c.id]
				}
			}
		}

		var lines []workLine
		for _, c := range tier {
			if c.work == nil || ident.isCited(c.work) {
				continue
			}
			if *since > 0 && c.work.Year < *since {
				continue
			}
			lines = append(lines, workLine{Work: *c.work, CitedByYours: c.citedByYours, CitesYours: c.citesYours})
		}
		slices.SortStableFunc(lines, func(a, b workLine) int { return b.Work.CitedByCount - a.Work.CitedByCount })
		picked = append(picked, lines...)
	}
	if len(picked) > *n {
		picked = picked[:*n]
	}

	held, err := loadHeld(s)
	if err != nil {
		return fmt.Errorf("related: %w", err)
	}
	for i := range picked {
		picked[i].Held = held.key(&picked[i].Work)
	}

	outcome := "ok"
	if len(picked) == 0 {
		outcome = "no-hits"
	}
	logOpenAlex(s, "related", path, outcome, len(picked), time.Since(start))

	if *asJSON {
		out := struct {
			Total      int        `json:"total"`
			Resolved   int        `json:"resolved"`
			Unresolved []string   `json:"unresolved"`
			Works      []jsonWork `json:"works"`
		}{Total: len(picked), Resolved: nResolved, Unresolved: unresolved, Works: make([]jsonWork, len(picked))}
		if out.Unresolved == nil {
			out.Unresolved = []string{}
		}
		for i, l := range picked {
			out.Works[i] = toJSONWork(l)
			if *short {
				out.Works[i].Abstract = ""
			}
		}
		data, err := json.Marshal(out, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("related: %w", err)
		}
		_, err = fmt.Printf("%s\n", data)
		return err
	}
	fmt.Printf("resolved: %d of %d entries\n", nResolved, len(keyed))
	if len(unresolved) > 0 {
		fmt.Printf("unresolved: %s\n", strings.Join(unresolved, ", "))
	}
	return printWorks(os.Stdout, len(picked), picked, *short, false)
}
