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
	"os"
	"path/filepath"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/pdfid"
	"seehuhn.de/go/paper/internal/resolve"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{
		"ingest", "identify PDF files and move them into the store", runIngest})
}

// ingestPages is how many pages of a PDF are searched for a DOI or an
// arXiv stamp. The stamps live on the first page; a second and third page
// cost little and catch a cover sheet in front of the real first page.
const ingestPages = 3

// ingestTitleMinScore is the TitleSimilarity an -into file's title must
// reach against the entry's title to count as verified. It matches the
// tier-3 acceptance threshold in pdfid.
const ingestTitleMinScore = 0.8

// ingestSource is the provenance recorded for a file that ingest moved in
// from the local filesystem rather than downloading.
const ingestSource = "ingest"

// sinceFormats are the timestamp layouts -since accepts: RFC3339, and a
// local-time form without zone for convenience.
var sinceFormats = []string{time.RFC3339, "2006-01-02T15:04:05"}

// runIngest implements the "paper ingest" command: it takes PDF files that
// are already on disk, works out which paper each one is, and moves it
// into the store.
//
// The file arguments are always explicit. What happens to them depends on
// the flags: -into attaches the single surviving file to an existing entry
// after verifying that it really is that paper; -doi or -arxiv name the
// paper outright and skip identification; otherwise each file is
// identified on its own, resolved online, and given a fresh draft entry.
//
// Nothing is ever downloaded: we already hold the file, so the online
// services are consulted for metadata only.
func runIngest(args []string) error {
	fs, storeFlag := newFlagSet("ingest")
	since := fs.String("since", "", "ignore files last modified before this time (RFC3339, or 2006-01-02T15:04:05 in local time)")
	into := fs.String("into", "", "attach the single surviving file to this existing entry")
	doiFlag := fs.String("doi", "", "skip identification: the file is the paper with this DOI")
	arxivFlag := fs.String("arxiv", "", "skip identification: the file is this arXiv e-print")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("ingest: parsing arguments: %w", err)
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("ingest: specify at least one PDF file to ingest")
	}
	override := *doiFlag != "" || *arxivFlag != ""
	if *doiFlag != "" && *arxivFlag != "" {
		return fmt.Errorf("ingest: -doi and -arxiv name the same file twice; use one of them")
	}
	if override && *into != "" {
		return fmt.Errorf("ingest: -into attaches to an existing entry, -doi/-arxiv create a new one; use one of them")
	}
	if override && fs.NArg() != 1 {
		return fmt.Errorf("ingest: -doi and -arxiv say what one file is, but %d files were given", fs.NArg())
	}

	files, err := ingestFiles(fs.Args(), *since)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}

	s, err := store.Open(*storeFlag)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	in := &ingester{
		store: s,
		email: cfg.Email,
		api:   &http.Client{Timeout: apiTimeout},
		now:   time.Now(),
	}

	switch {
	case *into != "":
		err = in.ingestInto(*into, files)
	case len(files) == 0:
		// Every file was filtered out by -since, which ingestFiles has
		// already reported. There is nothing left to do, and nothing failed.
		return nil
	default:
		err = in.ingestBatch(files, *doiFlag, *arxivFlag)
	}
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return nil
}

// ingester carries the state shared by the ingest branches.
type ingester struct {
	store *store.Store
	email string
	api   *http.Client // for metadata requests; ingest downloads nothing
	now   time.Time    // one timestamp for every log entry of this run
}

// crossref returns a Crossref client for this run.
func (in *ingester) crossref() *sources.Crossref {
	return &sources.Crossref{BaseURL: crossrefBase, Client: in.api, Email: in.email}
}

// ingestFile is one candidate file, with the modification time that
// -since filters on and that the survivor listing reports.
type ingestFile struct {
	path    string
	modTime time.Time
}

// ingestFiles stats the named files and drops the ones last modified
// before the -since cutoff. A dropped file is reported on stdout: it is a
// deliberate part of the run, not a failure. An empty since keeps every
// file.
func ingestFiles(paths []string, since string) ([]ingestFile, error) {
	var cutoff time.Time
	if since != "" {
		var err error
		cutoff, err = parseSince(since)
		if err != nil {
			return nil, err
		}
	}

	files := make([]ingestFile, 0, len(paths))
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("%s is a directory; ingest takes PDF files", path)
		}
		if since != "" && fi.ModTime().Before(cutoff) {
			fmt.Printf("skipping %s: last modified %s, before %s\n",
				path, fi.ModTime().Format(time.RFC3339), cutoff.Format(time.RFC3339))
			continue
		}
		files = append(files, ingestFile{path: path, modTime: fi.ModTime()})
	}
	return files, nil
}

// parseSince parses a -since timestamp in any of the accepted layouts. A
// layout without a zone is read as local time, which is what a human
// typing "yesterday afternoon" means.
func parseSince(s string) (time.Time, error) {
	for _, layout := range sinceFormats {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse -since %q: use 2006-01-02T15:04:05 or an RFC3339 timestamp", s)
}

// ingestInto implements behavior branch 2: attach one file to an entry
// that already exists, but only after checking that the file really is
// that paper. A file which fails the check is left where it is.
func (in *ingester) ingestInto(key string, files []ingestFile) error {
	if len(files) != 1 {
		return intoCountError(key, files)
	}
	f := files[0]

	tier, err := in.ingestIntoOne(key, f)
	in.store.LogEvent(store.Event{
		Command: "ingest",
		Input:   "file",
		Ref:     filepath.Base(f.path),
		Tier:    tier,
		Outcome: eventOutcome(err),
	})
	return err
}

// ingestIntoOne does the actual work of ingestInto for the single
// surviving file; kept separate so ingestInto can log exactly one event
// per invocation regardless of the outcome.
func (in *ingester) ingestIntoOne(key string, f ingestFile) (int, error) {
	p, err := in.store.Load(key)
	if err != nil {
		return 0, err
	}

	doc, id, err := in.identify(f.path)
	if err != nil {
		return 0, err
	}

	filename, ok := verifyInto(p, doc, id)
	if !ok {
		return id.Tier, intoMismatchError(p, f, doc, id)
	}
	if _, err := attachFile(in.store, p, f.path, filename, ingestSource, in.now); err != nil {
		return id.Tier, err
	}
	fmt.Printf("ingested %s -> %s\n", f.path, p.Key)
	return id.Tier, nil
}

// verifyInto checks a file against the entry it is to be attached to and,
// when they agree, returns the name to store it under. The file passes if
// it carries the entry's arXiv ID, or the entry's DOI, or a title close
// enough to the entry's. Only an arXiv match changes the file name: a
// preprint is stored under its version-qualified arXiv name, everything
// else as the published PDF.
func verifyInto(p *store.Paper, doc *pdfid.DocText, id pdfid.ID) (string, bool) {
	if id.ArxivID != "" && p.Arxiv != nil && id.ArxivID == p.Arxiv.ID {
		version := id.Version
		if version <= 0 {
			version = p.Arxiv.Version
		}
		return arxivFileBase(id.ArxivID, version) + ".pdf", true
	}
	if id.DOI != "" && p.DOI != "" && strings.EqualFold(id.DOI, p.DOI) {
		return "published.pdf", true
	}
	if match.TitleSimilarity(p.Bibtex.Fields["title"], pdfTitle(doc, id)) >= ingestTitleMinScore {
		return "published.pdf", true
	}
	return "", false
}

// pdfTitle is the best title the identification found for a file: the
// tier-3 guess read off the first page's largest type, falling back to the
// Info dictionary's title.
func pdfTitle(doc *pdfid.DocText, id pdfid.ID) string {
	if id.Title != "" {
		return id.Title
	}
	if doc != nil {
		return doc.Title
	}
	return ""
}

// ingestBatch implements behavior branch 3: identify, resolve and create
// an entry for each file on its own. Every file is processed, whatever
// happened to the ones before it; the successes are reported on stdout as
// they happen, and the failures are collected into one error at the end,
// so that the command exits nonzero if any file was left behind.
func (in *ingester) ingestBatch(files []ingestFile, doiOverride, arxivOverride string) error {
	var failures []ingestFailure
	for _, f := range files {
		key, tier, err := in.ingestOne(f, doiOverride, arxivOverride)
		in.store.LogEvent(store.Event{
			Command: "ingest",
			Input:   "file",
			Ref:     filepath.Base(f.path),
			Tier:    tier,
			Outcome: eventOutcome(err),
		})
		if err != nil {
			failures = append(failures, ingestFailure{path: f.path, err: err})
			continue
		}
		fmt.Printf("ingested %s -> %s\n", f.path, key)
	}
	if len(failures) == 0 {
		return nil
	}
	return batchError(len(files), failures)
}

// ingestFailure is one file the batch could not ingest.
type ingestFailure struct {
	path string
	err  error
}

// ingestOne identifies a single file, resolves what it found online, and
// creates the entry the file is attached to. The overrides implement
// behavior branch 4: when the caller has already worked out what the file
// is, identification is skipped entirely.
func (in *ingester) ingestOne(f ingestFile, doiOverride, arxivOverride string) (string, int, error) {
	switch {
	case doiOverride != "":
		ref := sources.ParseRef(doiOverride)
		if ref.Kind != sources.RefDOI {
			return "", 0, fmt.Errorf("-doi %q is not a DOI", doiOverride)
		}
		key, err := in.ingestDOI(f, trimDOI(ref.DOI))
		return key, 0, err

	case arxivOverride != "":
		ref := sources.ParseRef(arxivOverride)
		if ref.Kind != sources.RefArxiv {
			return "", 0, fmt.Errorf("-arxiv %q is not an arXiv ID", arxivOverride)
		}
		key, err := in.ingestArxiv(f, ref.ArxivID, ref.Version)
		return key, 0, err
	}

	doc, id, err := in.identify(f.path)
	if err != nil {
		return "", 0, err
	}
	switch {
	case id.DOI != "":
		key, err := in.ingestDOI(f, id.DOI)
		return key, id.Tier, err
	case id.ArxivID != "":
		key, err := in.ingestArxiv(f, id.ArxivID, id.Version)
		return key, id.Tier, err
	default:
		return "", id.Tier, unidentifiedError(doc, id)
	}
}

// identify extracts what a PDF says about itself and runs the pdfid tiers
// over it. Tier 3 resolves its title guess through Crossref.
func (in *ingester) identify(path string) (*pdfid.DocText, pdfid.ID, error) {
	doc, err := pdfid.Extract(path, ingestPages)
	if err != nil {
		return nil, pdfid.ID{}, err
	}
	return doc, pdfid.Identify(doc, in.searchTitle), nil
}

// searchTitle is the pdfid.SearchFunc for tier 3: it runs the title guess
// through a Crossref bibliographic search and scores the top hit by how
// well its title matches the guess. Crossref's own relevance score is not
// comparable across queries, so the decision rests on the title alone.
func (in *ingester) searchTitle(titleGuess string) (doi, matchedTitle string, score float64, err error) {
	hits, err := in.crossref().Search(titleGuess, searchRows)
	if err != nil {
		return "", "", 0, err
	}
	if len(hits) == 0 || hits[0].DOI == "" {
		return "", "", 0, nil
	}
	top := hits[0]
	if len(top.Titles) > 0 {
		matchedTitle = top.Titles[0]
	}
	return top.DOI, matchedTitle, match.TitleSimilarity(titleGuess, matchedTitle), nil
}

// ingestDOI resolves a DOI through Crossref and attaches the file to the
// draft entry it creates. Unpaywall is not consulted: it exists to find a
// PDF, and we are holding one.
func (in *ingester) ingestDOI(f ingestFile, doi string) (string, error) {
	key, err := findDuplicate(in.store, doi, "")
	if err != nil {
		return "", err
	}
	if key != "" {
		return "", duplicateError("DOI "+doi, key)
	}

	work, err := in.crossref().Work(doi)
	if err != nil {
		return "", err
	}
	p, err := resolve.FromCrossref(work)
	if err != nil {
		return "", err
	}
	return in.createAndAttach(f, p, "published.pdf", "created from crossref record "+work.DOI)
}

// ingestArxiv resolves an arXiv ID through the arXiv API, merging in the
// published record when the preprint names one, and attaches the file
// under its version-qualified arXiv name.
func (in *ingester) ingestArxiv(f ingestFile, id string, version int) (string, error) {
	key, err := findDuplicate(in.store, "", id)
	if err != nil {
		return "", err
	}
	if key != "" {
		return "", duplicateError("arXiv:"+arxivEprintID(id, version), key)
	}

	entry, err := (&sources.Arxiv{BaseURL: arxivBase, Client: in.api}).ByID(arxivEprintID(id, version))
	if err != nil {
		return "", err
	}
	p, err := resolve.FromArxiv(entry)
	if err != nil {
		return "", err
	}

	if doi := strings.TrimSpace(entry.DOI); doi != "" {
		// The preprint names a published version, which may already be in
		// the store under that DOI — the arXiv ID scan above cannot see it.
		key, err := findDuplicate(in.store, doi, "")
		if err != nil {
			return "", err
		}
		if key != "" {
			return "", duplicateError("DOI "+doi, key)
		}
		p = mergePublished(in.crossref(), p, entry, doi)
	}

	af := newArxivFetch(entry, sources.Ref{Kind: sources.RefArxiv, ArxivID: id, Version: version})
	return in.createAndAttach(f, p, af.pdfName, "created from arXiv record "+af.eprintID)
}

// createAndAttach writes the resolved draft entry to the store and moves
// the file into it. A failed attach leaves the entry behind, so the error
// says so: the metadata is worth keeping, and the caller only has to
// place the file.
func (in *ingester) createAndAttach(f ingestFile, p *store.Paper, filename, detail string) (string, error) {
	if err := createDraft(in.store, p, in.now, "ingest", detail+", identified in "+f.path); err != nil {
		return "", err
	}
	if _, err := attachFile(in.store, p, f.path, filename, ingestSource, in.now); err != nil {
		return "", fmt.Errorf("the draft entry %s was created, but the file could not be moved into it: %w",
			p.Key, err)
	}
	return p.Key, nil
}

// duplicateError reports a file whose paper is already in the store.
func duplicateError(what, key string) error {
	return wrapOutcome("duplicate", fmt.Errorf("%s is already in the store as %s; use paper search %s to inspect it, "+
		"or paper ingest -into %s to attach this file to it", what, key, key, key))
}

// intoCountError reports that -into did not get the single file it needs,
// listing every survivor of the -since filter by name and modification
// time so that the caller can pick one.
func intoCountError(key string, files []ingestFile) error {
	if len(files) == 0 {
		return fmt.Errorf("-into %s needs exactly one file, but -since filtered out every one of them", key)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "-into %s needs exactly one file, but %d are left:\n", key, len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "  %s (last modified %s)\n", f.path, f.modTime.Format(time.RFC3339))
	}
	b.WriteString("Re-run with only the file that belongs to " + key +
		", or narrow -since until one file is left.")
	return errors.New(b.String())
}

// intoMismatchError reports that a file does not look like the entry it
// was to be attached to, spelling out both sides of the disagreement. The
// file stays where it is.
func intoMismatchError(p *store.Paper, f ingestFile, doc *pdfid.DocText, id pdfid.ID) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s does not look like %s, so it was left in place.\n", f.path, p.Key)

	b.WriteString("\nWhat the PDF says about itself:\n")
	describePDF(&b, doc, id)

	b.WriteString("\nWhat the entry says:\n")
	if p.DOI != "" {
		fmt.Fprintf(&b, "  doi:     %s\n", p.DOI)
	} else {
		b.WriteString("  doi:     (none recorded)\n")
	}
	if p.Arxiv != nil {
		fmt.Fprintf(&b, "  arxiv:   %s\n", arxivEprintID(p.Arxiv.ID, p.Arxiv.Version))
	}
	fmt.Fprintf(&b, "  title:   %s\n", p.Bibtex.Fields["title"])
	fmt.Fprintf(&b, "  author:  %s\n", p.Bibtex.Fields["author"])

	b.WriteString("\nIf this really is the paper, record the file by hand in " +
		p.Key + "'s paper.json; otherwise ingest it without -into.")
	return wrapOutcome("mismatch", errors.New(b.String()))
}

// unidentifiedError reports a file that none of the identification tiers
// could pin down, handing over everything that was gathered along the
// way — including the tier-3 title guess, which is all the evidence there
// is that the search was tried and came back empty.
func unidentifiedError(doc *pdfid.DocText, id pdfid.ID) error {
	var b strings.Builder
	b.WriteString("cannot tell which paper this is.\n")
	b.WriteString("What the PDF says about itself:\n")
	describePDF(&b, doc, id)
	b.WriteString("\nFind the paper's DOI or arXiv ID, then re-run:\n")
	b.WriteString("  paper ingest -doi <doi> <file>\n")
	b.WriteString("  paper ingest -arxiv <id> <file>")
	return wrapOutcome("unidentified", errors.New(b.String()))
}

// describePDF writes what identification learned about a file: the Info
// dictionary's title and author, the tier-3 title guess, and the
// scanned-file verdict. The title guess is reported whenever there is
// one, since a tier-3 search that errored and one that found nothing look
// alike from here, and the guess is what tells the reader what was tried.
func describePDF(b *strings.Builder, doc *pdfid.DocText, id pdfid.ID) {
	if id.DOI != "" {
		fmt.Fprintf(b, "  doi:     %s\n", id.DOI)
	}
	if id.ArxivID != "" {
		fmt.Fprintf(b, "  arxiv:   %s\n", arxivEprintID(id.ArxivID, id.Version))
	}
	if doc != nil && doc.Title != "" {
		fmt.Fprintf(b, "  info title:  %s\n", doc.Title)
	}
	if doc != nil && doc.Author != "" {
		fmt.Fprintf(b, "  info author: %s\n", doc.Author)
	}
	if id.Title != "" {
		fmt.Fprintf(b, "  title guess: %s\n", id.Title)
	}
	if id.Scanned {
		b.WriteString("  no page yielded any text: likely scanned, OCR needed\n")
	}
	if id.DOI == "" && id.ArxivID == "" && id.Title == "" &&
		(doc == nil || (doc.Title == "" && doc.Author == "")) {
		b.WriteString("  nothing: no DOI, no arXiv stamp, no title, no metadata\n")
	}
}

// batchError summarises the files a batch run could not ingest. Every
// failure is reported in full: a batch is typically run unattended, so
// this message is the only account of what happened.
func batchError(total int, failures []ingestFailure) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d files were left in place:\n", len(failures), total)
	for _, f := range failures {
		fmt.Fprintf(&b, "\n%s:\n%v\n", f.path, f.err)
	}
	return errors.New(strings.TrimRight(b.String(), "\n"))
}
