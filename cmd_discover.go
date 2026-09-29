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
	"strings"
	"time"
)

const discoverHelp = `usage: paper discover [options] <query> ...

Search OpenAlex for works on a topic, to find literature the store does
not hold yet. The words of the query are joined and matched against
titles, abstracts and other text. The store is only read, never changed
(apart from the event log).

Plain output starts with the line

    total: <matches>, shown: <listed>

followed by one line per work, most relevant first:

    <id> | <year> | <surname> | <title> | <venue> | cited <n> | doi:<doi> | arxiv:<id> | held:<key>

<id> is the OpenAlex ID (W...), <surname> is that of the first author,
and cited <n> is the citation count. The doi, arxiv and held fields
appear only when they apply; held:<key> names the store paper that
already holds the work. Unless -short is given, each line is followed by
an indented "abstract: ..." line ("abstract: none" if OpenAlex has none).

With -json the output is one object {"total": n, "works": [...]}; each
work has the fields id, doi, arxiv, year, authors, title, venue, type,
cited_by_count, abstract and held. Only doi, arxiv, venue, abstract and
held are left out when empty; -short also drops abstract.

options:
    -n <count>    list at most <count> works (default 25, at most 200)
    -since <year> only works published in <year> or later
    -short        leave out the abstracts
    -json         print the result as JSON
    -store <dir>  path to the paper store (overrides the configured store)

Anonymous OpenAlex requests are rate limited; a free key stored with
"paper init -openalex-key KEY <store dir>" removes the limit in practice.
`

func init() {
	commands = append(commands, command{
		name: "discover",
		desc: "search OpenAlex for literature on a topic",
		help: discoverHelp,
		run:  runDiscover,
	})
}

// runDiscover implements "paper discover": a full-text OpenAlex search
// whose results are marked with the store papers that already hold them.
func runDiscover(args []string) error {
	fs, storeFlag := newFlagSet("discover")
	n := fs.Int("n", 25, "list at most this many works")
	since := fs.Int("since", 0, "only works published in this year or later")
	short := fs.Bool("short", false, "leave out the abstracts")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("discover: parsing arguments: %w", err)
	}

	if err := checkNoTrailingFlags("discover", "query", fs.Args()); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("discover: no query given; supply the words to search for")
	}
	if *n < 1 {
		return fmt.Errorf("discover: -n must be at least 1, got %d", *n)
	}

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	start := time.Now()
	oa := newOpenAlex(cfg)
	defer warnLowBudget(os.Stderr, oa)
	works, total, err := oa.Search(query, *n, *since)
	if err != nil {
		logOpenAlexErr(s, oa, "discover", query, err, time.Since(start))
		return fmt.Errorf("discover: %w", err)
	}

	held, err := loadHeld(s)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	lines := make([]workLine, len(works))
	for i := range works {
		lines[i] = workLine{Work: works[i], Held: held.key(&works[i])}
	}

	outcome := "ok"
	if len(works) == 0 {
		outcome = "no-hits"
	}
	logOpenAlex(s, oa, "discover", query, outcome, len(works), time.Since(start))

	return printWorks(os.Stdout, total, lines, *short, *asJSON)
}
