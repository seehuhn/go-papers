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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
	"seehuhn.de/go/paper/internal/tex"
)

const searchHelp = `usage: paper search [options] [<term> ...]

Ranked metadata search over the whole store. Every term must match.
Terms are matched against metadata only, never against the contents of
held files. At least one term or one filter option is required.

Plain output is one line per hit:

    <key>  <Surname> (<year>)  <title>  [<holdings>] [draft] [deprecated:<file>]

The bracketed flags at the end of the line are the holdings value
(none, preprint, published, both), then "draft" while the entry has not
yet passed "paper check", and one "deprecated:<file>" per held file
marked deprecated. Long titles are truncated to keep lines short.

With -json the hits are printed instead as a JSON array of objects with
the fields key, score, authors, title, year, holdings, status, and
flags (the same draft/deprecated markers, as an array of strings).
Use -json whenever the output is parsed rather than read.

options:
    -holdings <h>  only papers whose holdings is <h>
    -json          print results as a JSON array
    -status <s>    only papers with this status (draft, clean)
    -store <dir>   path to the paper store (overrides the configured store)

An empty result means the store does not hold the paper; acquiring it
is "paper fetch"'s job.
`

func init() {
	commands = append(commands, command{
		name: "search",
		desc: "search the store for papers",
		help: searchHelp,
		run:  runSearch,
	})
}

// maxHitLineRunes is the maximum length, in runes, of one human-readable
// search result line. Long titles are truncated to keep lines within
// this budget.
const maxHitLineRunes = 100

// runSearch implements the "paper search" command: it finds every paper
// matching all of the given search terms (see store.Search), optionally
// narrowed by -holdings/-status, and prints the results either as plain
// text (one line per hit) or, with -json, as a JSON array.
func runSearch(args []string) error {
	fs, storeFlag := newFlagSet("search")
	jsonFlag := fs.Bool("json", false, "print results as a JSON array")
	holdingsFlag := fs.String("holdings", "", "only include papers with this holdings value")
	statusFlag := fs.String("status", "", "only include papers with this status")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("search: parsing arguments: %w", err)
	}

	terms := fs.Args()
	if len(terms) == 0 && *holdingsFlag == "" && *statusFlag == "" {
		return fmt.Errorf("search: no search terms given; supply one or more terms, or a -holdings/-status filter")
	}
	if *holdingsFlag != "" && !store.ValidHoldings(*holdingsFlag) {
		return fmt.Errorf("search: invalid -holdings %q; want one of none, preprint, published, both", *holdingsFlag)
	}
	if *statusFlag != "" && !store.ValidStatus(*statusFlag) {
		return fmt.Errorf("search: invalid -status %q; want one of draft, clean", *statusFlag)
	}

	s, _, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	start := time.Now()
	hits, err := s.Search(terms)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	filtered := make([]store.Hit, 0, len(hits))
	for _, h := range hits {
		if *holdingsFlag != "" && h.Paper.Holdings != *holdingsFlag {
			continue
		}
		if *statusFlag != "" && h.Paper.Status != *statusFlag {
			continue
		}
		filtered = append(filtered, h)
	}

	// Zero-hit searches are the interesting signal in the event log -
	// they show which papers agents look for and the store lacks - so
	// they get their own outcome instead of a bare hits count.
	outcome := "ok"
	if len(filtered) == 0 {
		outcome = "no-hits"
	}
	s.LogEvent(store.Event{
		Command:  "search",
		Ref:      strings.Join(terms, " "),
		Outcome:  outcome,
		Hits:     len(filtered),
		Duration: time.Since(start).Milliseconds(),
	})

	if *jsonFlag {
		return printSearchJSON(filtered)
	}
	printSearchHuman(filtered)
	return nil
}

// searchResult is the JSON representation of one search hit.
type searchResult struct {
	Key      string   `json:"key"`
	Score    float64  `json:"score"`
	Authors  string   `json:"authors"`
	Title    string   `json:"title"`
	Year     string   `json:"year"`
	Holdings string   `json:"holdings"`
	Status   string   `json:"status"`
	Flags    []string `json:"flags"`
}

// printSearchJSON writes hits to stdout as a JSON array, in the format
// documented for "paper search -json": authors and title are decoded to
// plain unicode text (tex.Decode), and flags carries "draft" plus one
// "deprecated:<file>" entry per deprecated version.
func printSearchJSON(hits []store.Hit) error {
	results := make([]searchResult, 0, len(hits))
	for _, h := range hits {
		p := h.Paper
		authors, _ := tex.Decode(p.Bibtex.Fields["author"])
		title, _ := tex.Decode(p.Bibtex.Fields["title"])
		results = append(results, searchResult{
			Key:      p.Key,
			Score:    h.Score,
			Authors:  authors,
			Title:    title,
			Year:     p.Bibtex.Fields["year"],
			Holdings: p.Holdings,
			Status:   p.Status,
			Flags:    hitFlags(p),
		})
	}

	data, err := json.Marshal(results, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("search: encoding JSON: %w", err)
	}
	os.Stdout.Write(data)
	fmt.Println()
	return nil
}

// hitFlags reports the flags to show for one paper in search output:
// "draft" if the paper's status is draft, plus one "deprecated:<file>"
// entry per deprecated version, in sorted filename order.
func hitFlags(p *store.Paper) []string {
	var flags []string
	if p.Status == "draft" {
		flags = append(flags, "draft")
	}

	deprecated := make([]string, 0, len(p.Versions))
	for filename, v := range p.Versions {
		if v.Deprecated {
			deprecated = append(deprecated, filename)
		}
	}
	sort.Strings(deprecated)
	for _, filename := range deprecated {
		flags = append(flags, "deprecated:"+filename)
	}

	return flags
}

// printSearchHuman prints one line per hit to stdout, in the format:
// "<key>  <Author> (<year>)  <title>  [<holdings>] [draft] [deprecated:<file>]",
// with the title truncated as needed to keep the line within
// maxHitLineRunes runes. The draft/deprecated markers use the same
// hitFlags logic as the JSON "flags" array.
func printSearchHuman(hits []store.Hit) {
	for _, h := range hits {
		fmt.Println(formatHitLine(h))
	}
}

// formatHitLine formats one search hit as a single human-readable line.
func formatHitLine(h store.Hit) string {
	p := h.Paper

	who := firstAuthorLastName(p)
	year := p.Bibtex.Fields["year"]
	whoYear := formatWhoYear(who, year)

	title, _ := tex.Decode(p.Bibtex.Fields["title"])
	holdings := fmt.Sprintf("[%s]", p.Holdings)

	prefixParts := []string{p.Key}
	if whoYear != "" {
		prefixParts = append(prefixParts, whoYear)
	}
	prefix := strings.Join(prefixParts, "  ") + "  "

	suffix := "  " + holdings
	for _, flag := range hitFlags(p) {
		suffix += fmt.Sprintf(" [%s]", flag)
	}

	// The title is truncated first to keep the whole line within
	// maxHitLineRunes runes; when the flags themselves are long (e.g.
	// several deprecated files), the line may still exceed the budget -
	// the flags are never dropped or truncated to make it fit.
	budget := maxHitLineRunes - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix)
	title = truncateRunes(title, budget)

	return prefix + title + suffix
}

// formatWhoYear formats a hit's "<Author> (<year>)" segment, gracefully
// degrading when either piece is missing.
func formatWhoYear(who, year string) string {
	switch {
	case who != "" && year != "":
		return fmt.Sprintf("%s (%s)", who, year)
	case who != "":
		return who
	case year != "":
		return fmt.Sprintf("(%s)", year)
	default:
		return ""
	}
}

// firstAuthorLastName returns the decoded last name of p's first author,
// or "" if the author field is empty or cannot be parsed.
func firstAuthorLastName(p *store.Paper) string {
	names, err := bibtex.ParseNames(p.Bibtex.Fields["author"])
	if err != nil || len(names) == 0 {
		return ""
	}
	last, _ := tex.Decode(names[0].Last)
	return last
}

// truncateRunes returns s trimmed to at most max runes, replacing any
// trimmed suffix with a single "…" character. If max <= 0, it returns "".
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
