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
	"slices"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

const chainHelp = `usage: paper chain [options] <id>...

Follow the citation graph backward and forward from several papers at
once, according to OpenAlex, and rank what turns up. Each <id> (an
"anchor") is an OpenAlex ID (W...), a DOI or an arXiv ID. The command
takes the works every anchor cites and the works that cite it, merges
them, and ranks each work by how many anchors it is linked to (in either
direction), so that works close to several anchors come first. The
anchors are never listed as results. The store is only read, never
changed (apart from the event log).

Plain output starts with one line per anchor:

    anchor: <work line> | refs <r> | citing <c> of <C>

where <r> is the number of the anchor's references OpenAlex could
resolve, <c> the number of citing works fetched (the 200 newest) and <C>
the number OpenAlex reports. Then comes

    total: <works>, shown: <listed>

and one line per work, best first:

    <id> | <year> | <surname> | <title> | <venue> | cited <n> | anchors <k> | bib <k> | doi:<doi> | arxiv:<id> | held:<key>

<id> is the OpenAlex ID (W...), <surname> is that of the first author,
and cited <n> is the citation count. anchors <k> is the number of
anchors the work is linked to. With -bib, bib <k> is the number of
bibliography entries the work cites or is cited by (see below); the doi, arxiv and
held fields appear only when they apply, and held:<key> names the store
paper that already holds the work. Works are ranked by anchors plus bib,
then by citation count. Unless -short is given, each work line is
followed by an indented "abstract: ..." line ("abstract: none" if
OpenAlex has none).

With -bib <refs.bib>, works the bibliography already cites (same DOI,
same arXiv ID or a title that matches at 0.9) are left out, and each
remaining work is counted against the bibliography: bib <k> is how many
of its entries the work is linked to, that is, it cites the entry or is
cited by it; an entry counts once. Each entry is looked up in OpenAlex
by its DOI (or, for an arXiv-only entry, its arXiv DOI), else by its
title, which counts only if the best hit matches it with similarity
0.9 or more. The line

    bib: resolved <r> of <m> entries

(after the anchor lines) tells how many entries were found; the counts
rest on those. Entries that are not found, or whose lookup fails, are
recorded in the event log and do not stop the run.

With -json the output is one object {"anchors": [...], "total": n,
"works": [...]}, with -bib also "bib": {"resolved": r, "entries": m}
after "anchors"; each anchor and each work has the fields id, doi,
arxiv, year, authors, title, venue, type, cited_by_count, abstract and
held; the anchors also have refs, citing and citing_total, and the works
anchors and, with -bib, bib. Only doi, arxiv, venue, abstract and held
are left out when empty; -short also drops abstract.

An anchor OpenAlex does not know is reported on stderr and recorded in
the event log; the command stops with an error only if it knows none of
the anchors.

options:
    -bib <file>   a .bib file: leave out the works it cites, and count links to it
    -n <count>    list at most <count> works (default 100)
    -since <year> only citing works published in <year> or later; the
                  references of the anchors are not restricted
    -short        leave out the abstracts
    -json         print the result as JSON
    -store <dir>  path to the paper store (overrides the configured store)

example:

    paper chain -short -n 20 10.1029/2024gl110068 10.1029/2025gl114611

Anonymous OpenAlex requests are rate limited; a free key stored with
"paper init -openalex-key KEY <store dir>" removes the limit in practice.
`

func init() {
	commands = append(commands, command{
		name: "chain",
		desc: "chain backward and forward from several papers, ranked",
		help: chainHelp,
		run:  runChain,
	})
}

// chainCitingLimit is how many of the newest citations of each anchor
// are fetched.
const chainCitingLimit = 200

// chainCand is a work linked to at least one anchor, with the counts that
// rank it.
type chainCand struct {
	work    sources.OpenAlexWork
	anchors int // anchors it is cited by or cites
	bib     int // bibliography works among its references
}

// chainAnchor is one anchor and what was fetched around it.
type chainAnchor struct {
	line        workLine
	refs        int // references resolved
	citing      int // citing works fetched
	citingTotal int // citing works OpenAlex reports
}

// jsonChainAnchor is the JSON form of one chainAnchor.
type jsonChainAnchor struct {
	jsonWork
	Refs        int `json:"refs"`
	Citing      int `json:"citing"`
	CitingTotal int `json:"citing_total"`
}

// chainBib is how much of the bibliography the bib counts rest on.
type chainBib struct {
	Resolved int `json:"resolved"`
	Entries  int `json:"entries"`
}

// runChain implements "paper chain": the references and the citing works
// of several anchors, merged and ranked by how many anchors and
// bibliography entries each is linked to.
func runChain(args []string) error {
	fs, storeFlag := newFlagSet("chain")
	bibPath := fs.String("bib", "", "a .bib file: leave out the works it cites, and count links to it")
	n := fs.Int("n", 100, "list at most this many works")
	since := fs.Int("since", 0, "only citing works published in this year or later")
	short := fs.Bool("short", false, "leave out the abstracts")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("chain: parsing arguments: %w", err)
	}
	if err := checkNoTrailingFlags("chain", "IDs", fs.Args()); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("chain: expected at least one ID (OpenAlex ID, DOI or arXiv ID)")
	}
	if *n < 1 {
		return fmt.Errorf("chain: -n must be at least 1, got %d", *n)
	}
	ids := fs.Args()

	var entries []relatedEntry
	if *bibPath != "" {
		var err error
		if entries, err = parseBib("chain", *bibPath); err != nil {
			return err
		}
	}

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("chain: %w", err)
	}

	start := time.Now()
	ref := strings.Join(ids, " ")
	oa := newOpenAlex(cfg)
	defer warnLowBudget(os.Stderr, oa)
	fail := func(err error) error {
		logOpenAlexErr(s, oa, "chain", ref, err, time.Since(start))
		return fmt.Errorf("chain: %w", err)
	}
	held, err := loadHeld(s)
	if err != nil {
		return fmt.Errorf("chain: %w", err)
	}

	// Fetch what each anchor cites and what cites it, and merge.
	var anchors []chainAnchor
	anchorIDs := map[string]bool{}
	cands := map[string]*chainCand{}
	for _, id := range ids {
		a, err := oa.Work(id)
		if errors.Is(err, sources.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "anchor not found: %s\n", id)
			logOpenAlexErr(s, oa, "chain", id,
				wrapOutcome("openalex-unknown", fmt.Errorf("OpenAlex does not know %s", id)), time.Since(start))
			continue
		}
		if err != nil {
			return fail(err)
		}
		if anchorIDs[a.ID] {
			continue
		}
		anchorIDs[a.ID] = true

		var refs []sources.OpenAlexWork
		if len(a.References) > 0 {
			if refs, err = oa.Works(a.References); err != nil {
				return fail(err)
			}
		}
		citing, total, err := oa.Citing(a.ID, chainCitingLimit, *since)
		if err != nil {
			return fail(err)
		}
		anchors = append(anchors, chainAnchor{
			line: workLine{Work: *a, Held: held.key(a)},
			refs: len(refs), citing: len(citing), citingTotal: total,
		})

		seen := map[string]bool{}
		for _, list := range [][]sources.OpenAlexWork{refs, citing} {
			for _, w := range list {
				if seen[w.ID] {
					continue
				}
				seen[w.ID] = true
				c := cands[w.ID]
				if c == nil {
					c = &chainCand{work: w}
					cands[w.ID] = c
				}
				c.anchors++
			}
		}
	}
	if len(anchors) == 0 {
		return wrapOutcome("openalex-unknown", errors.New("chain: no anchor found"))
	}
	for id := range anchorIDs {
		delete(cands, id)
	}

	bibResolved := 0
	if *bibPath != "" {
		if bibResolved, err = countBibLinks(oa, s, start, entries, cands); err != nil {
			return fail(err)
		}
	}

	ranked := make([]*chainCand, 0, len(cands))
	for _, c := range cands {
		ranked = append(ranked, c)
	}
	slices.SortFunc(ranked, func(a, b *chainCand) int {
		switch {
		case a.anchors+a.bib != b.anchors+b.bib:
			return b.anchors + b.bib - a.anchors - a.bib
		case a.work.CitedByCount != b.work.CitedByCount:
			return b.work.CitedByCount - a.work.CitedByCount
		}
		return strings.Compare(a.work.ID, b.work.ID)
	})
	total := len(ranked)
	ranked = ranked[:min(len(ranked), *n)]

	lines := make([]workLine, len(ranked))
	for i, c := range ranked {
		lines[i] = workLine{Work: c.work, Held: held.key(&c.work), Anchors: &c.anchors}
		if *bibPath != "" {
			lines[i].Bib = &c.bib
		}
	}

	outcome := "ok"
	if len(lines) == 0 {
		outcome = "no-hits"
	}
	logOpenAlex(s, oa, "chain", ref, outcome, len(lines), time.Since(start))

	if *asJSON {
		out := struct {
			Anchors []jsonChainAnchor `json:"anchors"`
			Bib     *chainBib         `json:"bib,omitzero"`
			Total   int               `json:"total"`
			Works   []jsonWork        `json:"works"`
		}{Total: total, Anchors: make([]jsonChainAnchor, len(anchors)), Works: make([]jsonWork, len(lines))}
		if *bibPath != "" {
			out.Bib = &chainBib{bibResolved, len(entries)}
		}
		for i, a := range anchors {
			out.Anchors[i] = jsonChainAnchor{toJSONWork(a.line), a.refs, a.citing, a.citingTotal}
			if *short {
				out.Anchors[i].Abstract = ""
			}
		}
		for i, l := range lines {
			out.Works[i] = toJSONWork(l)
			if *short {
				out.Works[i].Abstract = ""
			}
		}
		data, err := json.Marshal(out, jsontext.WithIndent("  "))
		if err != nil {
			return fmt.Errorf("chain: %w", err)
		}
		_, err = fmt.Printf("%s\n", data)
		return err
	}
	for _, a := range anchors {
		fmt.Printf("anchor: %s | refs %d | citing %d of %d\n", workText(a.line), a.refs, a.citing, a.citingTotal)
	}
	if *bibPath != "" {
		fmt.Printf("bib: resolved %d of %d entries\n", bibResolved, len(entries))
	}
	return printWorks(os.Stdout, total, lines, *short, false)
}

// countBibLinks drops from cands the works the bibliography cites, and sets
// the bib count of the others: the number of bibliography works each is
// linked to, in either direction (it cites the entry, or the entry cites
// it), an entry counting once. It returns how many entries it found in OpenAlex.
// The entries with a DOI, or an arXiv ID (by its arXiv DOI), are found by
// one DOI lookup; every other entry, and any the lookup missed, is
// resolved one by one as "paper related" does (DOI, arXiv ID, then title).
// A failed resolution counts as not found and is recorded in the event
// log.
func countBibLinks(oa *sources.OpenAlex, s *store.Store, start time.Time,
	entries []relatedEntry, cands map[string]*chainCand) (int, error) {
	ident := &bibIdentity{dois: map[string]bool{}, arxivs: map[string]bool{}}
	doiOf := make([]string, len(entries)) // lower-case DOI to look up, or ""
	var dois []string
	for i, e := range entries {
		ident.add(e)
		switch {
		case e.doi != "":
			doiOf[i] = strings.ToLower(e.doi)
		case e.arxiv != "":
			doiOf[i] = strings.ToLower(sources.ArxivDOI(normArxiv(e.arxiv)))
		}
		if doiOf[i] != "" {
			dois = append(dois, doiOf[i])
		}
	}
	for id, c := range cands {
		if ident.isCited(&c.work) {
			delete(cands, id)
		}
	}

	bibRefs := map[string]map[string]bool{} // entry work ID -> IDs it cites
	addBib := func(w *sources.OpenAlexWork) {
		refs := map[string]bool{}
		for _, r := range w.References {
			refs[r] = true
		}
		bibRefs[w.ID] = refs
	}
	byDOI := map[string]*sources.OpenAlexWork{} // lower-case DOI -> work
	if len(dois) > 0 {
		works, err := oa.WorksByDOI(dois)
		if err != nil {
			return 0, err
		}
		for i := range works {
			byDOI[strings.ToLower(works[i].DOI)] = &works[i]
		}
	}
	resolved := 0
	for i, e := range entries {
		if w, ok := byDOI[doiOf[i]]; ok && doiOf[i] != "" {
			addBib(w)
			resolved++
			continue
		}
		w, err := resolveEntry(oa, e)
		switch {
		case err != nil:
			logOpenAlexErr(s, oa, "chain", e.ref(), wrapOutcome("openalex-unresolved", err), time.Since(start))
		case w == nil:
			logOpenAlex(s, oa, "chain", e.ref(), "openalex-unresolved", 0, time.Since(start))
		default:
			addBib(w)
			resolved++
		}
	}

	for _, c := range cands {
		cites := map[string]bool{}
		for _, r := range c.work.References {
			cites[r] = true
		}
		for id, refs := range bibRefs {
			if cites[id] || refs[c.work.ID] {
				c.bib++
			}
		}
	}
	return resolved, nil
}
