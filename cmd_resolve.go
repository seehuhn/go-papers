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
	"net/http"
	"strconv"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/resolve"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
	"seehuhn.de/go/paper/internal/tex"
)

const resolveHelp = `usage: paper resolve [options] <ref>

Turn a description of a work into its identifiers. <ref> may be a DOI,
an arXiv ID or URL, or free text ("Author, Title, year"). Nothing is
created: the store gains an entry only through "paper fetch".

The output names what was found - doi, arxiv, isbn, then author, title,
year and type. A book gets its ISBN from Open Library: the edition
matching the year (print before e-book, hardcover before paperback),
with the other editions listed after it. When the store already holds
the work, a "store:" line names the entry and its holdings, and any
identifier the entry lacked is recorded on it, which the output says.

Free text that no single record clearly matches lists the candidates
and exits nonzero; re-run with one of their identifiers.

options:
    -store <dir>  path to the paper store (overrides the configured store)
`

func init() {
	commands = append(commands, command{
		name: "resolve",
		desc: "turn a description of a work into its identifiers",
		help: resolveHelp,
		run:  runResolve,
	})
}

// olTitleMinScore is the score an Open Library work's title must reach
// before resolve takes it to be the work being asked about - the
// match.TitleCoverage of a free-text reference, or the
// match.TitleSimilarity of an already-resolved title. Open Library's
// search is title-driven and answers generously, so the bar has to be
// high enough that a merely related book is not mistaken for the one
// wanted.
const olTitleMinScore = 0.8

// runResolve implements the "paper resolve" command: it resolves a
// reference (DOI, arXiv ID or URL, or free text) against the online
// metadata services and prints the identifiers it found. It writes no
// entry - that is "paper fetch" - so its whole product is the output,
// which the calling agent reads to decide what to do next.
func runResolve(args []string) (err error) {
	fs, storeFlag := newFlagSet("resolve")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("resolve: parsing arguments: %w", err)
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("resolve: specify a reference: a DOI, an arXiv ID or URL, or free text such as 'Hoeffding inequalities 1963'")
	}
	// Unquoted free text arrives as several arguments; a DOI or arXiv ID
	// arrives as one, and joining leaves it untouched.
	refStr := strings.Join(fs.Args(), " ")

	s, cfg, err := openStore(*storeFlag)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}

	ref := sources.ParseRef(refStr)
	r := &resolver{
		email:  cfg.Email,
		api:    &http.Client{Timeout: apiTimeout},
		source: refKindSource(ref.Kind),
		store:  s,
		now:    time.Now(),
	}

	start := time.Now()
	defer func() {
		s.LogEvent(store.Event{
			Command:  "resolve",
			Input:    refKindInput(ref.Kind),
			Ref:      strings.TrimSpace(refStr),
			Outcome:  eventOutcome(err),
			Detail:   eventDetail(err),
			Source:   r.source,
			Duration: time.Since(start).Milliseconds(),
		})
	}()

	return r.run(ref)
}

// resolver carries the state shared by the resolution branches.
type resolver struct {
	email  string
	api    *http.Client
	source string // the service that named the work, for the event log
	store  *store.Store
	now    time.Time
}

// crossref returns a Crossref client for this run.
func (r *resolver) crossref() *sources.Crossref {
	return &sources.Crossref{BaseURL: crossrefBase, Client: r.api, Email: r.email}
}

// openLibrary returns an Open Library client for this run.
func (r *resolver) openLibrary() *sources.OpenLibrary {
	return &sources.OpenLibrary{BaseURL: openLibraryBase, Client: r.api}
}

// run resolves one reference and prints what it found.
func (r *resolver) run(ref sources.Ref) error {
	p, workKey, err := r.resolveRef(ref)
	if err != nil {
		return err
	}

	var pick sources.OLEdition
	var others []sources.OLEdition
	var notes []string
	if wantsISBN(p, workKey) {
		var note string
		pick, others, note = r.editions(p, workKey)
		if note != "" {
			notes = append(notes, note)
		}
		if pick.ISBN != "" {
			// Open Library has looked at every edition of the work, so
			// its answer supersedes the single ISBN a Crossref record
			// happens to carry.
			p.ISBN = pick.ISBN
		}
	}

	reportResolved(p, pick, others, notes)
	return r.recordOnHeldWork(p)
}

// recordOnHeldWork looks up the store entry the resolved reference already
// names - by DOI or arXiv ID, then, failing that, by title and first
// author surname - and records on it any identifier resolve found that it
// lacked. The "store:" line is printed whenever a held entry is found,
// even when nothing about it changes, so that the agent always learns
// which entry the reference maps to.
func (r *resolver) recordOnHeldWork(p *store.Paper) error {
	var arxivID string
	if p.Arxiv != nil {
		arxivID = p.Arxiv.ID
	}
	key, err := findDuplicate(r.store, p.DOI, arxivID)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if key == "" {
		key, err = findByTitle(r.store, p.Bibtex.Fields["title"], firstSurname(p.Bibtex))
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
	}
	if key == "" {
		return nil
	}

	held, err := r.store.Load(key)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	fmt.Printf("%-8s %s\n", "store:", fmt.Sprintf("%s (holdings: %s)", held.Key, held.Holdings))

	details, err := held.RecordIdentifiers(p.DOI, p.ISBN)
	if err != nil {
		fmt.Printf("conflict: %v\n", err)
		return fmt.Errorf("resolve: %w", err)
	}
	if len(details) == 0 {
		return nil
	}

	held.AppendLog(r.now, "resolve", strings.Join(details, "; "))
	if err := r.store.Save(held); err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	for _, d := range details {
		// d already reads "recorded doi …" or "replaced arXiv DOI … by …",
		// per RecordIdentifiers's doc comment; no extra verb is added here.
		fmt.Printf("%s on %s\n", d, held.Key)
	}
	for _, problem := range store.CheckPaper(held) {
		fmt.Printf("check: %s\n", problem.Msg)
	}
	return nil
}

// resolveRef turns a parsed reference into a draft entry. workKey is the
// Open Library work key when Open Library was the record's source, and ""
// otherwise; it saves the ISBN lookup from searching for a work it has
// already found.
func (r *resolver) resolveRef(ref sources.Ref) (p *store.Paper, workKey string, err error) {
	switch ref.Kind {
	case sources.RefDOI:
		p, err := r.resolveDOI(trimDOI(ref.DOI))
		return p, "", err
	case sources.RefArxiv:
		p, err := r.resolveArxiv(ref)
		return p, "", err
	case sources.RefPDFURL:
		return nil, "", fmt.Errorf("resolve: %s names a file, not a work, and nothing is known about "+
			"the paper it holds until it has been downloaded; use \"paper fetch\" for that", ref.URL)
	default:
		return r.resolveText(ref.Text)
	}
}

// resolveDOI resolves a DOI through Crossref.
func (r *resolver) resolveDOI(doi string) (*store.Paper, error) {
	work, err := r.crossref().Work(doi)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	p, err := resolve.FromCrossref(work)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	return p, nil
}

// resolveArxiv resolves an arXiv ID, merging in the published record
// when the preprint names a DOI, exactly as fetch does: the published
// metadata is the entry, and the preprint contributes the eprint fields.
func (r *resolver) resolveArxiv(ref sources.Ref) (*store.Paper, error) {
	entry, err := (&sources.Arxiv{BaseURL: arxivBase, Client: r.api}).ByID(arxivEprintID(ref.ArxivID, ref.Version))
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	p, err := resolve.FromArxiv(entry)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	if doi := strings.TrimSpace(entry.DOI); doi != "" {
		p = mergePublished(r.crossref(), p, entry, doi)
	}
	return p, nil
}

// resolveText resolves free text: a Crossref search first, accepted only
// when one hit clearly wins, then an Open Library search for the books
// Crossref does not index. When neither pins a single work down, the
// candidates from both are handed to the caller to choose between.
func (r *resolver) resolveText(query string) (*store.Paper, string, error) {
	hits, err := r.crossref().Search(query, searchRows)
	if err != nil {
		return nil, "", fmt.Errorf("resolve: %w", err)
	}
	if best := autoAccept(hits, query); best != nil {
		// Re-resolve through /works/<doi>: a search item can carry less
		// than the canonical record.
		p, err := r.resolveDOI(best.DOI)
		return p, "", err
	}

	works, olErr := r.openLibrary().Search(query, "", searchRows)
	if w, ok := onlyCloseMatch(works, query); ok {
		p, err := paperFromOLWork(w)
		if err != nil {
			return nil, "", fmt.Errorf("resolve: %w", err)
		}
		r.source = "openlibrary"
		return p, w.Key, nil
	}
	return nil, "", r.ambiguousWork(query, hits, works, olErr)
}

// onlyCloseMatch returns the single Open Library work whose title the
// reference text carries. The test is containment, not similarity: a
// reference is typed as "Author, Title, year", so it holds words the
// title does not, and match.TitleCoverage is the asymmetric measure that
// ignores them. It reports false when no work clears the bar and also
// when several do: free text that fits two books resolves to neither.
func onlyCloseMatch(works []sources.OLWork, text string) (sources.OLWork, bool) {
	var found sources.OLWork
	n := 0
	for _, w := range works {
		if match.TitleCoverage(w.Title, text) >= olTitleMinScore {
			found, n = w, n+1
		}
	}
	return found, n == 1
}

// ambiguousWork reports that free text could not be resolved, listing
// the Crossref and Open Library candidates so that the agent can pick one
// and re-run resolve with an unambiguous identifier.
func (r *resolver) ambiguousWork(query string, hits []*sources.CrossrefWork, works []sources.OLWork, olErr error) error {
	candidates := make([]sources.Candidate, 0, len(hits)+len(works))
	for _, h := range hits {
		candidates = append(candidates, crossrefCandidate(h))
	}
	for _, w := range works {
		candidates = append(candidates, olCandidate(w))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "resolve: cannot resolve %q to a single work.\n", query)
	if len(candidates) == 0 {
		b.WriteString("Neither Crossref nor Open Library found anything for this query.\n")
	} else {
		b.WriteString("\nCandidates:\n")
		writeCandidateList(&b, candidates)
	}
	if olErr != nil {
		fmt.Fprintf(&b, "\nopen library search failed: %v\n", olErr)
	}

	b.WriteString("\nPick the intended work and re-run resolve with its identifier, e.g.\n")
	fmt.Fprintf(&b, "  paper resolve %s\n", exampleDOI(candidates))
	b.WriteString("  paper resolve arXiv:2412.05039\n")
	b.WriteString("If none of these is right, search the open web for the work's DOI or arXiv ID first.")
	return wrapOutcome("ambiguous", errors.New(b.String()))
}

// olCandidate converts an Open Library work into a Candidate, so that it
// can be listed alongside the Crossref hits. Open Library records no DOI
// and no containing venue, so both stay empty.
func olCandidate(w sources.OLWork) sources.Candidate {
	return sources.Candidate{
		Source:  "openlibrary",
		Title:   w.Title,
		Authors: w.Authors,
		Year:    w.FirstYear,
	}
}

// paperFromOLWork builds the draft entry for a work only Open Library
// knows about. Open Library reports natural-order plain-unicode author
// names, the same shape arXiv uses, so resolve.BibtexAuthors splits and
// encodes them - by the same rule, so that the same book keys the same
// way whichever service named it.
func paperFromOLWork(w sources.OLWork) (*store.Paper, error) {
	if w.Title == "" {
		return nil, fmt.Errorf("open library work %s: missing title", w.Key)
	}
	author, err := resolve.BibtexAuthors(w.Authors)
	if err != nil {
		return nil, fmt.Errorf("open library work %s: %w", w.Key, err)
	}

	fields := map[string]string{
		"author": author,
		"title":  bibtex.BraceTitle(tex.Encode(w.Title)),
	}
	if w.FirstYear != 0 {
		fields["year"] = strconv.Itoa(w.FirstYear)
	}

	return &store.Paper{
		Status:   "draft",
		Holdings: "none",
		Bibtex:   bibtex.Entry{Type: "book", Fields: fields},
	}, nil
}

// wantsISBN reports whether the resolved work should have its ISBN looked
// up at Open Library: every book, and every work Open Library named in
// the first place, whose edition list is where the ISBN lives.
func wantsISBN(p *store.Paper, workKey string) bool {
	if workKey != "" {
		return true
	}
	switch p.Bibtex.Type {
	case "book", "inbook":
		return true
	}
	return false
}

// editions finds the Open Library edition to report for a resolved work,
// together with the alternatives, next-best first. workKey names the work
// when it is already known; otherwise Open Library is searched for the
// work by title and first-author surname, and the first work whose title
// is close enough is used.
//
// Open Library being unhelpful is not an error: the work may simply not
// be a book it holds, and a Crossref-supplied ISBN still stands. A failed
// request is reported as a note instead, so that a missing ISBN is never
// silently confused with a service being down.
func (r *resolver) editions(p *store.Paper, workKey string) (pick sources.OLEdition, others []sources.OLEdition, note string) {
	ol := r.openLibrary()

	if workKey == "" {
		// Open Library wants plain text, not bibtex encoding, so the
		// title and surname are decoded before they go into the query.
		title, _ := tex.Decode(p.Bibtex.Fields["title"])
		surname, _ := tex.Decode(firstSurname(p.Bibtex))
		works, err := ol.Search(title, surname, searchRows)
		if err != nil {
			return pick, nil, "open library search failed: " + err.Error()
		}
		for _, w := range works {
			if match.TitleSimilarity(title, w.Title) >= olTitleMinScore {
				workKey = w.Key
				break
			}
		}
		if workKey == "" {
			return pick, nil, ""
		}
	}

	list, err := ol.Editions(workKey)
	if err != nil {
		return pick, nil, "open library editions lookup failed: " + err.Error()
	}
	year, _ := strconv.Atoi(p.Bibtex.Fields["year"])
	// A false ok leaves pick zero, and an empty pick.ISBN is already how
	// the caller learns that Open Library supplied nothing.
	pick, others, _ = sources.PickEdition(list, p.Bibtex.Fields["edition"], year)
	return pick, others, ""
}

// reportResolved prints what resolve found: one labelled line per identifier and
// per bibliographic field, in the order an agent reads them, with the
// lines that have no value left out entirely.
func reportResolved(p *store.Paper, pick sources.OLEdition, others []sources.OLEdition, notes []string) {
	line := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Printf("%-8s %s\n", label+":", value)
	}

	line("doi", p.DOI)
	if p.Arxiv != nil {
		line("arxiv", arxivEprintID(p.Arxiv.ID, p.Arxiv.Version))
	}
	if pick.ISBN != "" {
		line("isbn", editionLabel(pick))
		for _, e := range others {
			line("also", editionLabel(e))
		}
	} else {
		line("isbn", p.ISBN)
	}
	line("author", p.Bibtex.Fields["author"])
	line("title", p.Bibtex.Fields["title"])
	line("year", p.Bibtex.Fields["year"])
	line("type", p.Bibtex.Type)
	for _, n := range notes {
		line("note", n)
	}
}

// editionLabel renders one Open Library edition: its ISBN, followed by
// whichever of the physical format and the year Open Library knows
// ("9780521006019 (hardcover 1994)"), which is what tells two editions of
// the same book apart at a glance.
func editionLabel(e sources.OLEdition) string {
	var parts []string
	if e.Format != "" {
		parts = append(parts, e.Format)
	}
	if e.Year != 0 {
		parts = append(parts, strconv.Itoa(e.Year))
	}
	if len(parts) == 0 {
		return e.ISBN
	}
	return e.ISBN + " (" + strings.Join(parts, " ") + ")"
}
