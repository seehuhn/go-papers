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
	"sort"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/match"
	"seehuhn.de/go/paper/internal/pdfid"
	"seehuhn.de/go/paper/internal/resolve"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

const ingestHelp = `usage: paper ingest [options] <file.pdf> ...

Take PDF files that are already on disk, work out which paper each one
is, and MOVE them into the store. Nothing is ever downloaded; the
online services are consulted for metadata only. For a PDF that is
still behind a URL, use "paper fetch <url>" instead.

Without flags, each file is identified from its own contents, resolved
online, and given a fresh draft entry.

With no file arguments, "paper ingest" picks up downloads the user was
asked for: it scans ~/Desktop, ~/Downloads and the current directory for
PDFs newer than the oldest draft entry that has no file yet, verifies each
against those entries by title, and moves every unambiguous match into
the store. Files that match nothing are left alone. A file that cannot
be identified from its own contents, or that matches several entries,
is reported and left where it is; the command exits nonzero whenever
anything was left unresolved or nothing was moved.

-into attaches to an existing entry and is all-or-nothing: exactly one
given file must survive stat'ing, and a file that cannot be read blocks
the run just as a second survivor would. With zero or several survivors
the command fails and lists what it found. The surviving file is
verified against the entry by title before it is moved, so the wrong
PDF is refused rather than filed.

In bash, glob patterns such as ~/Downloads/*.pdf need
"shopt -s nullglob" first: without it an unmatched pattern is passed
through as a literal path, counts as a failed file, and blocks an
-into run.

-doi and -arxiv skip identification and say outright what the single
given file is.

options:
    -arxiv <id>    skip identification: the file is this arXiv e-print
    -doi <doi>     skip identification: the file is the paper with this DOI
    -into <key>    attach the single surviving file to this existing entry
    -store <dir>   path to the paper store (overrides the configured store)
`

func init() {
	commands = append(commands, command{
		name: "ingest",
		desc: "identify PDF files and move them into the store",
		help: ingestHelp,
		run:  runIngest,
	})
}

// ingestPages is how many pages of a PDF are searched for a DOI or an
// arXiv stamp. The stamps live on the first page; a second and third page
// cost little and catch a cover sheet in front of the real first page.
const ingestPages = 3

// ingestTitleMinScore is the TitleSimilarity an -into file's title must
// reach against the entry's title to count as verified. The -into
// verification is deliberately laxer than tier-3 identification because
// the user already named the target entry; this check is only a safety net.
const ingestTitleMinScore = 0.8

// ingestSource is the provenance recorded for a file that ingest moved in
// from the local filesystem rather than downloading.
const ingestSource = "ingest"

// runIngest implements the "paper ingest" command: it takes PDF files that
// are already on disk, works out which paper each one is, and moves it
// into the store.
//
// With no file arguments and none of -into/-doi/-arxiv, it runs the
// pickup instead (see (*ingester).pickup): it hunts ~/Desktop,
// ~/Downloads and the working directory for files the user was asked to
// download, rather than requiring them to be named explicitly.
//
// Otherwise the file arguments are explicit. What happens to them depends
// on the flags: -into attaches the single surviving file to an existing
// entry after verifying that it really is that paper; -doi or -arxiv name
// the paper outright and skip identification; otherwise each file is
// identified on its own, resolved online, and given a fresh draft entry.
//
// Nothing is ever downloaded: we already hold the file, so the online
// services are consulted for metadata only.
func runIngest(args []string) error {
	fs, storeFlag := newFlagSet("ingest")
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
		if *into != "" || *doiFlag != "" || *arxivFlag != "" {
			return fmt.Errorf("ingest: -into, -doi and -arxiv name what a file is, but no file was given; " +
				"omit them to run the pickup instead")
		}
		s, cfg, err := openStore(*storeFlag)
		if err != nil {
			return fmt.Errorf("ingest: %w", err)
		}
		in := &ingester{
			store: s,
			email: cfg.Email,
			api:   &http.Client{Timeout: apiTimeout},
			now:   time.Now(),
		}
		if err := in.pickup(pickupDirs()); err != nil {
			return fmt.Errorf("ingest: %w", err)
		}
		return nil
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

	files, failures := ingestFiles(fs.Args())

	s, cfg, err := openStore(*storeFlag)
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
		err = in.ingestInto(*into, files, failures)
	default:
		err = in.ingestBatch(files, failures, *doiFlag, *arxivFlag)
	}
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return nil
}

// ingester carries the state shared by the ingest branches, and by
// fetch's PDF-URL branch, which reuses the identification pipeline on the
// file it has just downloaded.
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

// ingestFile is one candidate file, with the modification time that the
// -into survivor listing and the pickup's candidate scan report.
type ingestFile struct {
	path    string
	modTime time.Time

	// source is the provenance recorded for the file once it is attached.
	// It is ingestSource for a file already on disk, and the URL it came
	// from for one that fetch downloaded. Where a file came from is what
	// a later audit has to work from, so a download records the URL
	// rather than losing it behind a temporary path.
	source string
}

// downloaded reports whether the file was fetched from a URL rather than
// found on the local filesystem. A zero-value ingestFile is not a
// download: only an explicitly recorded non-"ingest" source counts.
func (f ingestFile) downloaded() bool {
	return f.source != "" && f.source != ingestSource
}

// logAction is the entry-log action for the command that produced this
// file.
func (f ingestFile) logAction() string {
	if f.downloaded() {
		return "fetch"
	}
	return "ingest"
}

// displayName is what user-facing messages call the file: the URL it
// was downloaded from, or its path for a local file. A download's temp
// path is already gone by the time anyone reads the message.
func (f ingestFile) displayName() string {
	if f.downloaded() {
		return f.source
	}
	return f.path
}

// origin describes where the file came from, for the entry log. A
// downloaded file names its URL: its path is a temporary directory that
// will not exist by the time anyone reads the log.
func (f ingestFile) origin() string {
	if f.downloaded() {
		return "downloaded from " + f.source
	}
	return "identified in " + f.path
}

// ingestFiles stats the named files. A path that cannot be stat'ed
// (dangling symlink, permissions, an unmaterialized cloud placeholder) or
// that names a directory is not an abort: it comes back as a per-file
// failure, leaving the caller free to carry on with everything else that
// did stat cleanly.
func ingestFiles(paths []string) ([]ingestFile, []ingestFailure) {
	files := make([]ingestFile, 0, len(paths))
	var failures []ingestFailure
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			failures = append(failures, ingestFailure{path: path, err: err})
			continue
		}
		if fi.IsDir() {
			failures = append(failures, ingestFailure{
				path: path,
				err:  fmt.Errorf("%s is a directory; ingest takes PDF files", path),
			})
			continue
		}
		files = append(files, ingestFile{path: path, modTime: fi.ModTime(), source: ingestSource})
	}
	return files, failures
}

// ingestInto implements behavior branch 2: attach one file to an entry
// that already exists, but only after checking that the file really is
// that paper. A file which fails the check is left where it is.
//
// -into keeps the old all-or-nothing semantics: it needs exactly one
// survivor, so a stat failure among the arguments blocks the run just as
// a second surviving file would, rather than being ingested per-file the
// way a batch run's failures are.
func (in *ingester) ingestInto(key string, files []ingestFile, failures []ingestFailure) error {
	if len(files) != 1 || len(failures) != 0 {
		return intoCountError(key, files, failures)
	}
	f := files[0]

	tier, err := in.ingestIntoOne(key, f)
	in.store.LogEvent(store.Event{
		Command: "ingest",
		Input:   "file",
		Ref:     filepath.Base(f.path),
		Tier:    tier,
		Outcome: eventOutcome(err),
		Detail:  eventDetail(err),
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

	if err := in.attach(p, key, f, doc, id); err != nil {
		return id.Tier, err
	}
	fmt.Printf("ingested %s -> %s\n", f.displayName(), p.Key)
	return id.Tier, nil
}

// attach is the shared tail of ingestion once a file has been identified:
// it verifies that f really is the paper the entry at key describes, and,
// if so, moves it into the store and records its provenance. It is used
// both by -into ingestion and by the pickup, so the verify-then-move logic
// is written once. A file that does not verify is left in place, reported
// by intoMismatchError; the caller decides what to print on success.
func (in *ingester) attach(p *store.Paper, key string, f ingestFile, doc *pdfid.DocText, id pdfid.ID) error {
	p.Key = key
	filename, ok := verifyInto(p, doc, id)
	if !ok {
		return intoMismatchError(p, f, doc, id)
	}
	_, err := attachFile(in.store, p, f.path, filename, f.source, in.now)
	return err
}

// verifyInto checks a file against the entry it is to be attached to and,
// when they agree, returns the name to store it under. The file passes if
// it carries the entry's arXiv ID, or the entry's DOI, or a title close
// enough to the entry's.
//
// The name follows what the file is, not the route by which it was
// verified: any file carrying an arXiv stamp is a preprint and is stored
// under its version-qualified arXiv name, so that RecomputeHoldings reads
// it as one. Filing such a file as published.pdf — which a title match
// against a published entry used to do — would claim a version of record
// the store does not hold.
//
// An arXiv identity the entry does not yet record is written onto p here,
// so that the attach which follows saves it to paper.json: the file is
// the only evidence there is, and losing it would leave holdings and
// metadata disagreeing.
func verifyInto(p *store.Paper, doc *pdfid.DocText, id pdfid.ID) (string, bool) {
	sameArxiv := id.ArxivID != "" && p.Arxiv != nil && id.ArxivID == p.Arxiv.ID
	sameDOI := id.DOI != "" && p.DOI != "" && strings.EqualFold(id.DOI, p.DOI)
	sameTitle := match.TitleSimilarity(p.Bibtex.Fields["title"], pdfTitle(doc, id)) >= ingestTitleMinScore
	if !sameArxiv && !sameDOI && !sameTitle {
		return "", false
	}

	if id.ArxivID == "" {
		return "published.pdf", true
	}

	version := id.Version
	switch {
	case p.Arxiv == nil:
		p.Arxiv = &store.ArxivRef{ID: id.ArxivID, Version: id.Version}
	case sameArxiv && version <= 0:
		// The file names the e-print but not which version of it; the
		// entry already knows.
		version = p.Arxiv.Version
	}
	return arxivFileBase(id.ArxivID, version) + ".pdf", true
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

// pickupTimeLayout is the layout a paper.json log entry's When field uses
// (see store.LogEntry and store.Paper.AppendLog).
const pickupTimeLayout = "2006-01-02T15:04:05"

// pickupDirs returns the directories the pickup scans: the user's Desktop
// and Downloads, and the current working directory (the caller's
// workspace). A directory that cannot be resolved is left out rather than
// failing the run; scanPDFs skips a directory that does not exist anyway.
func pickupDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Desktop"), filepath.Join(home, "Downloads"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	return dirs
}

// pickupEntry is one store entry awaiting a file: holdings "none". created
// is read from the entry's first log line; it is the zero time, with
// createdUnknown set, when the log is empty or the line does not parse -
// such an entry can be matched against any candidate, however old.
type pickupEntry struct {
	key            string
	paper          *store.Paper
	created        time.Time
	createdUnknown bool
}

// awaitingEntries returns every store entry with holdings "none" and
// status "draft", the entries the pickup tries to fill. A holdings-none
// entry that has passed "paper check" and been promoted to "clean" is a
// deliberate metadata-only entry, not one awaiting a file, and must not
// be offered a match.
func (in *ingester) awaitingEntries() ([]pickupEntry, error) {
	papers, err := in.store.LoadAll()
	if err != nil {
		return nil, err
	}
	var out []pickupEntry
	for _, p := range papers {
		if p.Holdings != "none" || p.Status != "draft" {
			continue
		}
		created, unknown := entryCreated(p)
		out = append(out, pickupEntry{key: p.Key, paper: p, created: created, createdUnknown: unknown})
	}
	return out, nil
}

// entryCreated reads an entry's creation time off its first log line. An
// empty log or an unparsable When is reported back as unknown rather than
// failing the run: the entry is simply treated as always eligible.
func entryCreated(p *store.Paper) (created time.Time, unknown bool) {
	if len(p.Log) == 0 {
		return time.Time{}, true
	}
	t, err := time.ParseInLocation(pickupTimeLayout, p.Log[0].When, time.Local)
	if err != nil {
		return time.Time{}, true
	}
	return t, false
}

// pickupCandidate is one PDF found while scanning for downloads.
type pickupCandidate struct {
	path    string
	modTime time.Time
}

// scanPDFs lists every regular file ending in ".pdf" (case-insensitive)
// directly inside dirs, whose modification time is not before oldest. A
// directory that cannot be read (does not exist, or any other error) is
// skipped silently: the pickup runs the same whether or not ~/Desktop
// happens to exist. Candidates are returned sorted by path, and
// deduplicated by absolute path in case two of dirs coincide.
func scanPDFs(dirs []string, oldest time.Time) []pickupCandidate {
	var out []pickupCandidate
	seen := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			abs, err := filepath.Abs(path)
			if err != nil {
				abs = path
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			if info.ModTime().Before(oldest) {
				continue
			}
			out = append(out, pickupCandidate{path: path, modTime: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// pickupMatch is the outcome of identifying one candidate: which awaiting
// entries it verified against (sorted by key), or, when identification
// itself failed, why.
type pickupMatch struct {
	candidate    pickupCandidate
	doc          *pdfid.DocText
	id           pdfid.ID
	keys         []string
	unidentified bool
	reason       string
}

// matchCandidates identifies each candidate and checks it against every
// awaiting entry whose creation time is not after the candidate's modtime,
// using the same verifyInto check -into uses.
func (in *ingester) matchCandidates(cands []pickupCandidate, awaiting []pickupEntry) []pickupMatch {
	matches := make([]pickupMatch, 0, len(cands))
	for _, c := range cands {
		doc, id, err := in.identify(c.path)
		title := pdfTitle(doc, id)
		if err != nil {
			matches = append(matches, pickupMatch{candidate: c, unidentified: true, reason: err.Error()})
			continue
		}
		if title == "" && id.DOI == "" && id.ArxivID == "" {
			matches = append(matches, pickupMatch{
				candidate: c, unidentified: true,
				reason: "no DOI, no arXiv stamp, and no title could be read from it",
			})
			continue
		}

		var keys []string
		for _, a := range awaiting {
			if a.created.After(c.modTime) {
				continue
			}
			if _, ok := verifyInto(a.paper, doc, id); ok {
				keys = append(keys, a.key)
			}
		}
		sort.Strings(keys)
		matches = append(matches, pickupMatch{candidate: c, doc: doc, id: id, keys: keys})
	}
	return matches
}

// pickupReport is the outcome of applyMatches: the printable report,
// whether the run counts as a success (behaviour 10: at least one file
// moved, nothing left ambiguous or unidentified), and the counts pickup
// uses to build a short error when it is not: how many files were moved,
// and how many candidates were left unresolved (unidentified, ambiguous,
// or failed to move).
type pickupReport struct {
	text            string
	ok              bool
	movedCount      int
	unresolvedCount int
}

// applyMatches decides what to do with each match, attaches the safe
// ones, and builds the report. A candidate is safe to attach when it
// verifies against exactly one awaiting entry and that entry verifies
// against no other candidate; everything else is reported and left in
// place. One event per candidate is logged along the way.
func (in *ingester) applyMatches(awaiting []pickupEntry, matches []pickupMatch) pickupReport {
	byKey := make(map[string]*pickupEntry, len(awaiting))
	suitors := make(map[string][]string) // key -> matching candidate paths
	for i := range awaiting {
		byKey[awaiting[i].key] = &awaiting[i]
	}
	for _, m := range matches {
		for _, k := range m.keys {
			suitors[k] = append(suitors[k], m.candidate.path)
		}
	}

	var b strings.Builder
	resolved := make(map[string]bool)
	var movedCount int
	var noMatchCount int
	var unresolved bool     // an ambiguity or a failure that keeps the exit nonzero
	var unresolvedCount int // how many candidates triggered it, for the closing error

	for _, m := range matches {
		switch {
		case m.unidentified:
			fmt.Fprintf(&b, "unidentified: %s: %s; if it is one of the entries below, "+
				"record it by hand (see paper help schema)\n", m.candidate.path, m.reason)
			unresolved = true
			unresolvedCount++
			in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "unidentified"})

		case len(m.keys) == 0:
			noMatchCount++
			in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "no-match"})

		case len(m.keys) > 1:
			fmt.Fprintf(&b, "ambiguous: %s matches %s\n", m.candidate.path, strings.Join(m.keys, ", "))
			unresolved = true
			unresolvedCount++
			in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "ambiguous"})

		case len(suitors[m.keys[0]]) > 1:
			// Reported once per key below; still needs its own event.
			unresolved = true
			unresolvedCount++
			in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "ambiguous"})

		default:
			key := m.keys[0]
			f := ingestFile{path: m.candidate.path, modTime: m.candidate.modTime, source: ingestSource}
			err := in.attach(byKey[key].paper, key, f, m.doc, m.id)
			if err != nil {
				// Verification already succeeded here; this is the move
				// itself failing (permissions, a full disk, ...), not a
				// question of what the file is. It gets its own report
				// line and its own event outcome rather than borrowing
				// "unidentified", which would misdescribe a file that was
				// in fact identified.
				fmt.Fprintf(&b, "failed: %s -> %s: %s\n", m.candidate.path, key, err)
				unresolved = true
				unresolvedCount++
				in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "error"})
				continue
			}
			fmt.Fprintf(&b, "moved %s -> %s\n", m.candidate.path, key)
			movedCount++
			resolved[key] = true
			in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Ref: m.candidate.path, Outcome: "ok"})
		}
	}

	var ambiguousKeys []string
	for k, paths := range suitors {
		if len(paths) > 1 {
			ambiguousKeys = append(ambiguousKeys, k)
		}
	}
	sort.Strings(ambiguousKeys)
	for _, k := range ambiguousKeys {
		paths := append([]string(nil), suitors[k]...)
		sort.Strings(paths)
		fmt.Fprintf(&b, "ambiguous: %s matched by %s\n", k, strings.Join(paths, ", "))
	}

	if noMatchCount > 0 {
		fmt.Fprintf(&b, "%d other PDFs matched no awaiting entry\n", noMatchCount)
	}

	var still []string
	for _, a := range awaiting {
		if resolved[a.key] {
			continue
		}
		who := formatWhoYear(firstAuthorLastName(a.paper), a.paper.Bibtex.Fields["year"])
		still = append(still, fmt.Sprintf("%s  %s  %s", a.key, who, a.paper.Bibtex.Fields["title"]))
	}
	if len(still) == 0 {
		b.WriteString("nothing left awaiting\n")
	} else {
		b.WriteString("still awaiting:\n")
		for _, line := range still {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	ok := movedCount > 0 && !unresolved && len(ambiguousKeys) == 0
	return pickupReport{text: b.String(), ok: ok, movedCount: movedCount, unresolvedCount: unresolvedCount}
}

// pickup implements "paper ingest" with no arguments: it scans dirs for
// PDFs the user was asked to download and files the unambiguous ones,
// following the contract in the design spec (behaviours 1-12 there).
func (in *ingester) pickup(dirs []string) error {
	awaiting, err := in.awaitingEntries()
	if err != nil {
		return err
	}

	var notes strings.Builder
	for _, a := range awaiting {
		if a.createdUnknown {
			fmt.Fprintf(&notes, "note: %s has no readable creation time; treating it as always eligible\n", a.key)
		}
	}

	if len(awaiting) == 0 {
		fmt.Print(notes.String())
		fmt.Println("no entry is awaiting a file")
		return nil
	}

	oldest := awaiting[0].created
	for _, a := range awaiting[1:] {
		if a.created.Before(oldest) {
			oldest = a.created
		}
	}

	cands := scanPDFs(dirs, oldest)
	if len(cands) == 0 {
		in.store.LogEvent(store.Event{Command: "ingest", Input: "pickup", Outcome: "no-candidates"})
	}

	matches := in.matchCandidates(cands, awaiting)
	report := in.applyMatches(awaiting, matches)

	fmt.Print(notes.String())
	fmt.Print(report.text)
	if report.ok {
		return nil
	}
	if report.movedCount == 0 {
		return errors.New("pickup moved nothing")
	}
	return fmt.Errorf("pickup left %d candidates unresolved", report.unresolvedCount)
}

// ingestBatch implements behavior branch 3: identify, resolve and create
// an entry for each file on its own. Every file is processed, whatever
// happened to the ones before it; the successes are reported on stdout as
// they happen, and the failures are collected into one error at the end,
// so that the command exits nonzero if any file was left behind.
//
// statFailures are paths ingestFiles could not even stat (or found to be
// directories); they could never reach ingestOne, so they are logged and
// folded into the closing error here rather than stopping the batch.
func (in *ingester) ingestBatch(files []ingestFile, statFailures []ingestFailure, doiOverride, arxivOverride string) error {
	failures := append([]ingestFailure(nil), statFailures...)
	for _, f := range statFailures {
		in.store.LogEvent(store.Event{
			Command: "ingest",
			Input:   "file",
			Ref:     filepath.Base(f.path),
			Outcome: eventOutcome(f.err),
			Detail:  eventDetail(f.err),
		})
	}
	for _, f := range files {
		key, tier, err := in.ingestOne(f, doiOverride, arxivOverride)
		in.store.LogEvent(store.Event{
			Command: "ingest",
			Input:   "file",
			Ref:     filepath.Base(f.path),
			Tier:    tier,
			Outcome: eventOutcome(err),
			Detail:  eventDetail(err),
		})
		if err != nil {
			failures = append(failures, ingestFailure{path: f.path, err: err})
			continue
		}
		fmt.Printf("ingested %s -> %s\n", f.displayName(), key)
	}
	if len(failures) == 0 {
		return nil
	}
	return batchError(len(files)+len(statFailures), failures)
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
		// -doi trusts the given DOI absolutely: no identification, no
		// second-guessing. But the file is in hand, so its XMP metadata
		// is still extracted and used to fill gaps in the drafted entry,
		// the same way the normal path does; only the DOI/arXiv tiers of
		// pdfid.Identify are skipped, not the extraction underneath them.
		doc, err := pdfid.Extract(f.path, ingestPages)
		if err != nil {
			return "", 0, err
		}
		key, err := in.ingestDOI(f, trimDOI(ref.DOI), doc.Prism)
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
		key, err := in.ingestDOI(f, id.DOI, doc.Prism)
		return key, id.Tier, err
	case id.ArxivID != "":
		key, err := in.ingestArxiv(f, id.ArxivID, id.Version)
		return key, id.Tier, err
	default:
		return "", id.Tier, unidentifiedError(f, doc, id)
	}
}

// identify extracts what a PDF says about itself and runs the pdfid tiers
// over it. Tier 3 resolves its title guess through Crossref; tier 2
// confirms a prose DOI candidate's existence through the handle system
// (see (*ingester).handle).
func (in *ingester) identify(path string) (*pdfid.DocText, pdfid.ID, error) {
	doc, err := pdfid.Extract(path, ingestPages)
	if err != nil {
		return nil, pdfid.ID{}, err
	}
	cfg := pdfid.Config{Search: in.searchTitle, ValidateDOI: in.handle().Exists}
	return doc, pdfid.Identify(doc, cfg), nil
}

// handle returns a Handle client for this run, following the constructor
// pattern in cmd_fetch.go's auditor.
func (in *ingester) handle() *sources.Handle {
	return &sources.Handle{BaseURL: handleBase, Client: in.api}
}

// searchTitle is the pdfid.SearchFunc for tier 3: it runs the title guess
// through a Crossref bibliographic search and scores the top hit by how
// well its title matches the guess. Crossref's own relevance score is not
// comparable across queries, so the score rests on the title alone; the
// hit's first author and publication year are carried along too, so that
// tier 3 can corroborate the title match against the document's own text
// before accepting it (see pdfid.SearchHit).
func (in *ingester) searchTitle(titleGuess string) (pdfid.SearchHit, error) {
	hits, err := in.crossref().Search(titleGuess, searchRows)
	if err != nil {
		return pdfid.SearchHit{}, err
	}
	if len(hits) == 0 || hits[0].DOI == "" {
		return pdfid.SearchHit{}, nil
	}
	top := hits[0]
	var matchedTitle string
	if len(top.Titles) > 0 {
		matchedTitle = top.Titles[0]
	}
	hit := pdfid.SearchHit{
		DOI:   top.DOI,
		Title: matchedTitle,
		Score: match.TitleSimilarity(titleGuess, matchedTitle),
		Year:  top.Published.Year(),
	}
	if len(top.Authors) > 0 {
		hit.Author = top.Authors[0].Family
	}
	return hit, nil
}

// ingestDOI resolves a DOI through Crossref and attaches the file to the
// draft entry it creates. Unpaywall is not consulted: it exists to find a
// PDF, and we are holding one.
//
// prism is the publisher metadata read out of the file's XMP packet, or
// nil when the file carried none (and when -doi skipped identification
// altogether). Crossref stays authoritative; the PRISM fields only fill
// gaps the Crossref record left, which saves the polish pass from
// looking up a journal name or page range by hand.
func (in *ingester) ingestDOI(f ingestFile, doi string, prism *pdfid.PrismInfo) (string, error) {
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
	resolve.FillFromPrism(p, prism)
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
	if err := createDraft(in.store, p, in.now, f.logAction(), detail+", "+f.origin()); err != nil {
		return "", err
	}
	if _, err := attachFile(in.store, p, f.path, filename, f.source, in.now); err != nil {
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
// listing every given file by name and modification time, and every path
// that could not even be stat'ed, so that the caller can pick one.
func intoCountError(key string, files []ingestFile, failures []ingestFailure) error {
	total := len(files) + len(failures)

	var b strings.Builder
	fmt.Fprintf(&b, "-into %s needs exactly one file, but %d are left:\n", key, total)
	for _, f := range files {
		fmt.Fprintf(&b, "  %s (last modified %s)\n", f.path, f.modTime.Format(time.RFC3339))
	}
	for _, f := range failures {
		fmt.Fprintf(&b, "  %s: %v\n", f.path, f.err)
	}
	b.WriteString("Re-run with only the file that belongs to " + key + ".")
	return errors.New(b.String())
}

// intoMismatchError reports that a file does not look like the entry it
// was to be attached to, spelling out both sides of the disagreement. The
// file stays where it is.
func intoMismatchError(p *store.Paper, f ingestFile, doc *pdfid.DocText, id pdfid.ID) error {
	var b strings.Builder
	if f.downloaded() {
		// There is no file to leave in place: the download lives in a
		// temporary directory that goes away when the command returns.
		fmt.Fprintf(&b, "%s does not look like %s, so nothing was attached.\n", f.source, p.Key)
	} else {
		fmt.Fprintf(&b, "%s does not look like %s, so it was left in place.\n", f.path, p.Key)
	}

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
func unidentifiedError(f ingestFile, doc *pdfid.DocText, id pdfid.ID) error {
	var b strings.Builder
	b.WriteString("cannot tell which paper this is.\n")
	b.WriteString("What the PDF says about itself:\n")
	describePDF(&b, doc, id)
	b.WriteString("\nFind the paper's DOI or arXiv ID, then re-run:\n")
	if f.downloaded() {
		// The download went to a temporary directory that is already
		// gone, so the retry has to start from the URL again. There is no
		// -arxiv here: an arXiv URL never reaches this branch, it is
		// recognized as an arXiv reference before anything is downloaded.
		fmt.Fprintf(&b, "  paper fetch -doi <doi> %s\n", f.source)
		fmt.Fprintf(&b, "  paper fetch -into <key> %s", f.source)
	} else {
		b.WriteString("  paper ingest -doi <doi> <file>\n")
		b.WriteString("  paper ingest -arxiv <id> <file>")
	}
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
