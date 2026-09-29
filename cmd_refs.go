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
	"flag"
	"fmt"
	"os"
	"time"

	"seehuhn.de/go/paper/internal/sources"
)

const refsHelp = `usage: paper refs [options] <id>

List the works that a paper cites, as OpenAlex records them, to find the
literature behind a result. <id> is an OpenAlex ID (W...), a DOI or an
arXiv ID. The store is only read, never changed (apart from the event
log).

Plain output starts with the line

    work: <work line>

which describes the paper itself, followed by

    total: <references>, shown: <listed>

and one line per reference, in the order OpenAlex lists them:

    <id> | <year> | <surname> | <title> | <venue> | cited <n> | doi:<doi> | arxiv:<id> | held:<key>

<id> is the OpenAlex ID (W...), <surname> is that of the first author,
and cited <n> is the citation count. The doi, arxiv and held fields
appear only when they apply; held:<key> names the store paper that
already holds the work. The work line has the same fields. Unless -short
is given, each reference line is followed by an indented "abstract: ..."
line ("abstract: none" if OpenAlex has none).

If OpenAlex lists no references for a journal article, the output is
"total: 0, shown: 0" and the line

    OpenAlex lists no references for this article.

That is a gap in OpenAlex, not proof that the article cites nothing.

With -json the output is one object {"work": {...}, "total": n,
"works": [...]}; work and each entry of works have the fields id, doi,
arxiv, year, authors, title, venue, type, cited_by_count, abstract and
held. Only doi, arxiv, venue, abstract and held are left out when empty;
-short also drops abstract.

options:
    -short        leave out the abstracts
    -json         print the result as JSON
    -store <dir>  path to the paper store (overrides the configured store)

Anonymous OpenAlex requests are rate limited; a free key stored with
"paper init -openalex-key KEY" removes the limit in practice.
`

func init() {
	commands = append(commands, command{
		name: "refs",
		desc: "list the works a paper cites, according to OpenAlex",
		help: refsHelp,
		run:  runRefs,
	})
}

// runRefs implements "paper refs": the reference list of one work, as
// OpenAlex holds it, marked with the store papers that already hold each
// entry.
func runRefs(args []string) error {
	fs, storeFlag := newFlagSet("refs")
	short := fs.Bool("short", false, "leave out the abstracts")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("refs: parsing arguments: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("refs: expected exactly one ID (OpenAlex ID, DOI or arXiv ID), got %d arguments", fs.NArg())
	}
	id := fs.Arg(0)

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("refs: %w", err)
	}

	start := time.Now()
	oa := newOpenAlex(cfg)
	work, err := oa.Work(id)
	if errors.Is(err, sources.ErrNotFound) {
		err = wrapOutcome("openalex-unknown", fmt.Errorf("refs: OpenAlex does not know %s", id))
		logOpenAlexErr(s, "refs", id, err, time.Since(start))
		return err
	}
	if err != nil {
		logOpenAlexErr(s, "refs", id, err, time.Since(start))
		return fmt.Errorf("refs: %w", err)
	}

	var works []sources.OpenAlexWork
	if len(work.References) > 0 {
		works, err = oa.Works(work.References)
		if err != nil {
			logOpenAlexErr(s, "refs", id, err, time.Since(start))
			return fmt.Errorf("refs: %w", err)
		}
	}

	held, err := loadHeld(s)
	if err != nil {
		return fmt.Errorf("refs: %w", err)
	}
	head := workLine{Work: *work, Held: held.key(work)}
	lines := make([]workLine, len(works))
	for i := range works {
		lines[i] = workLine{Work: works[i], Held: held.key(&works[i])}
	}

	outcome := "ok"
	noRefs := len(work.References) == 0 && work.Type == "article"
	switch {
	case noRefs:
		outcome = "openalex-no-refs"
	case len(works) == 0:
		outcome = "no-hits"
	}
	logOpenAlex(s, "refs", id, outcome, len(works), time.Since(start))

	err = printWorksOf(os.Stdout, &head, len(work.References), lines, *short, *asJSON)
	if err == nil && noRefs && !*asJSON {
		fmt.Println("OpenAlex lists no references for this article.")
	}
	return err
}
