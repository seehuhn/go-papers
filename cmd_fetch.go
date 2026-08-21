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
	"regexp"
	"strconv"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/resolve"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{
		"fetch", "resolve a reference online and download what is scriptable", runFetch})
}

// Base URLs of the online services fetch talks to. These are variables
// rather than constants so that tests can point them at httptest servers;
// nothing else ever changes them.
var (
	crossrefBase      = "https://api.crossref.org"
	arxivBase         = "https://export.arxiv.org"
	unpaywallBase     = "https://api.unpaywall.org"
	zbmathBase        = "https://api.zbmath.org"
	dblpBase          = "https://dblp.org"
	arxivDownloadBase = "https://arxiv.org"
)

// apiTimeout bounds a metadata request; downloadTimeout bounds a file
// download, which legitimately takes much longer for a large PDF.
const (
	apiTimeout      = 30 * time.Second
	downloadTimeout = 5 * time.Minute
)

// searchRows is the number of hits requested from each search service.
const searchRows = 5

// scoreMargin is how much better than the runner-up Crossref's top hit
// must score before free-text resolution accepts it without asking.
const scoreMargin = 1.3

// runFetch implements the "paper fetch" command: it resolves a reference
// (DOI, arXiv ID or URL, or free text) against the online metadata
// services, creates a draft entry, and downloads whatever can be fetched
// reliably by script — an Unpaywall-supplied open-access PDF, or the
// arXiv PDF plus its tex source.
//
// When there is no scriptable download route the entry is still created,
// but the command fails with everything it has learned about the paper.
// That split — entry persisted, nonzero exit, rich message — is the
// contract with the calling agent, which takes the hunt from there.
func runFetch(args []string) error {
	fs, storeFlag := newFlagSet("fetch")
	dryRun := fs.Bool("dry-run", false, "resolve and report, without writing to the store or downloading")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("fetch: parsing arguments: %w", err)
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("fetch: specify a reference: a DOI, an arXiv ID or URL, or free text such as 'Hoeffding inequalities 1963'")
	}
	// Unquoted free text arrives as several arguments; a DOI or arXiv ID
	// arrives as one, and joining leaves it untouched.
	refStr := strings.Join(fs.Args(), " ")

	s, err := store.Open(*storeFlag)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	f := &fetcher{
		store:  s,
		email:  cfg.Email,
		dryRun: *dryRun,
		api:    &http.Client{Timeout: apiTimeout},
		dl:     &http.Client{Timeout: downloadTimeout},
		now:    time.Now(),
	}

	ref := sources.ParseRef(refStr)
	switch ref.Kind {
	case sources.RefDOI:
		return f.fetchDOI(trimDOI(ref.DOI))
	case sources.RefArxiv:
		return f.fetchArxiv(ref)
	default:
		return f.fetchText(ref.Text)
	}
}

// fetcher carries the state shared by the three resolution branches.
type fetcher struct {
	store  *store.Store
	email  string
	dryRun bool
	api    *http.Client // for metadata requests
	dl     *http.Client // for file downloads
	now    time.Time    // one timestamp for every log entry of this run
}

// crossref returns a Crossref client for this run.
func (f *fetcher) crossref() *sources.Crossref {
	return &sources.Crossref{BaseURL: crossrefBase, Client: f.api, Email: f.email}
}

// trimDOI removes trailing prose punctuation from a DOI. ParseRef's DOI
// pattern is deliberately permissive about the suffix, so a DOI copied
// out of a sentence can arrive with the sentence's punctuation attached.
func trimDOI(doi string) string {
	return strings.TrimRight(doi, ".,;)")
}

// fetchDOI implements behavior branch 1: resolve a DOI through Crossref
// and download an open-access PDF when Unpaywall knows one.
func (f *fetcher) fetchDOI(doi string) error {
	stop, err := f.stopOnDuplicate("DOI "+doi, doi, "")
	if stop {
		return err
	}

	work, err := f.crossref().Work(doi)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return f.fetchWork(work)
}

// fetchWork creates the draft entry for a Crossref work and downloads its
// open-access PDF, if there is one. It is shared by the DOI branch and by
// free-text resolution once a hit has been accepted.
func (f *fetcher) fetchWork(work *sources.CrossrefWork) error {
	// Plan B does not look for an arXiv preprint of a DOI-resolved work:
	// the arXiv API has no DOI lookup, and matching by title is not
	// reliable enough to link automatically. Fetching by arXiv ID (branch
	// 2) does merge the published record in the other direction; linking
	// the preprint to a published entry is left to a later pass.
	p, err := resolve.FromCrossref(work)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	pdfURL, verdict := f.unpaywallRoute(work.DOI)

	if f.dryRun {
		var downloads []plannedDownload
		if pdfURL != "" {
			downloads = append(downloads, plannedDownload{url: pdfURL, name: "published.pdf"})
		}
		f.reportDryRun(p, []string{"unpaywall: " + verdict}, downloads)
		return nil
	}

	if err := f.create(p, "created from crossref record "+work.DOI); err != nil {
		return err
	}

	if pdfURL == "" {
		return f.noOARouteError(p, work, verdict)
	}
	p, err = f.attachDownload(p, pdfURL, "published.pdf", "unpaywall")
	if err != nil {
		// The entry is in the store; only the file is missing, so this is
		// the same hand-off as having no OA route at all.
		return f.noOARouteError(p, work, fmt.Sprintf("%s, but the download failed: %v", verdict, err))
	}
	fmt.Printf("downloaded published.pdf from %s\n", pdfURL)
	return nil
}

// fetchArxiv implements behavior branch 2: resolve an arXiv ID, merge in
// the published record when the preprint names a DOI, and download both
// the PDF and the tex source.
func (f *fetcher) fetchArxiv(ref sources.Ref) error {
	queryID := arxivEprintID(ref.ArxivID, ref.Version)
	stop, err := f.stopOnDuplicate("arXiv:"+queryID, "", ref.ArxivID)
	if stop {
		return err
	}

	entry, err := (&sources.Arxiv{BaseURL: arxivBase, Client: f.api}).ByID(queryID)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	p, err := resolve.FromArxiv(entry)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if entry.DOI != "" {
		// The preprint names a published version: prefer its metadata.
		// A Crossref failure here is not fatal — the arXiv-only entry is
		// still useful — but it must be visible in Pending.
		if work, werr := f.crossref().Work(entry.DOI); werr != nil {
			p.Pending = addPending(p.Pending, fmt.Sprintf(
				"crossref lookup of %s failed (%v); published metadata is missing", entry.DOI, werr))
		} else if published, perr := resolve.FromCrossref(work); perr != nil {
			p.Pending = addPending(p.Pending, fmt.Sprintf(
				"crossref record %s is unusable (%v); published metadata is missing", entry.DOI, perr))
		} else {
			p = resolve.Merge(published, entry)
		}
	}

	// arXiv normally reports a version; the fallbacks keep the naming
	// scheme sane if it ever does not.
	version := entry.Version
	if version <= 0 {
		version = ref.Version
	}
	id := entry.ID
	if id == "" {
		id = ref.ArxivID
	}
	base := arxivFileBase(id, version)
	pdfName := base + ".pdf"
	pdfURL := arxivPDFURL(id, version)
	srcURL := arxivSourceURL(id, version)

	if f.dryRun {
		f.reportDryRun(p, nil, []plannedDownload{
			{url: pdfURL, name: pdfName},
			{url: srcURL, name: base + "/"},
		})
		return nil
	}

	if err := f.create(p, "created from arXiv record "+arxivEprintID(id, version)); err != nil {
		return err
	}

	p, err = f.attachDownload(p, pdfURL, pdfName, "arxiv")
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	fmt.Printf("downloaded %s from %s\n", pdfName, pdfURL)

	return f.fetchArxivSource(p, srcURL, base, pdfName)
}

// fetchArxivSource downloads and extracts the arXiv e-print source into
// the paper directory. The extracted directory is not a single file, so
// it cannot go through store.Attach: it is recorded in p.Versions here
// instead. A PDF-only submission is not a failure; it leaves a note on
// the PDF version saying that no tex source exists.
func (f *fetcher) fetchArxivSource(p *store.Paper, srcURL, base, pdfName string) error {
	destDir := filepath.Join(f.store.Dir(p.Key), base)
	err := resolve.FetchSource(f.dl, srcURL, destDir, f.email)

	switch {
	case errors.Is(err, resolve.ErrPDFOnly):
		if p.Versions == nil {
			p.Versions = make(map[string]store.Version)
		}
		v := p.Versions[pdfName]
		v.Note = "arXiv submission is PDF-only, no tex source"
		p.Versions[pdfName] = v
		p.AppendLog(f.now, "fetch", "no tex source on arXiv: "+resolve.ErrPDFOnly.Error())
		fmt.Printf("no tex source: %v\n", resolve.ErrPDFOnly)
	case err != nil:
		return fmt.Errorf("fetch: downloading arXiv source: %w", err)
	default:
		if p.Versions == nil {
			p.Versions = make(map[string]store.Version)
		}
		p.Versions[base] = store.Version{
			Acquired: f.now.Format("2006-01-02"),
			Source:   "arxiv",
		}
		store.RecomputeHoldings(p)
		p.AppendLog(f.now, "attach", base+"/ from arxiv")
		fmt.Printf("extracted tex source into %s/\n", base)
	}

	if err := f.store.Save(p); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// fetchText implements behavior branch 3: resolve free text through a
// Crossref search, accepting the top hit only when it is unambiguous.
func (f *fetcher) fetchText(query string) error {
	hits, err := f.crossref().Search(query, searchRows)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	if best := autoAccept(hits, query); best != nil {
		fmt.Printf("crossref: accepting %s (score %.1f)\n", best.DOI, best.Score)
		// Re-resolve through /works/<doi>: a search item can carry less
		// than the canonical record, and this keeps the duplicate check
		// and the rest of branch 1 in one place.
		return f.fetchDOI(best.DOI)
	}

	return f.ambiguousError(query, hits)
}

// yearRe matches a plausible publication year (1800-2099) in free text.
var yearRe = regexp.MustCompile(`\b(1[89][0-9]{2}|20[0-9]{2})\b`)

// autoAccept returns the Crossref hit that free-text resolution may take
// without asking, or nil when the query does not pin one down. The top hit
// qualifies if it outscores the runner-up by scoreMargin (or stands alone)
// and no year named in the query contradicts it. A query naming no year is
// decided by the score ratio alone: there is nothing to contradict, and
// that gate — together with draft status and the duplicate check — is the
// guard against a wrong entry.
func autoAccept(hits []*sources.CrossrefWork, query string) *sources.CrossrefWork {
	if len(hits) == 0 {
		return nil
	}
	top := hits[0]
	if top.DOI == "" || top.Score <= 0 {
		return nil
	}
	if len(hits) > 1 && top.Score < scoreMargin*hits[1].Score {
		return nil
	}

	years := yearRe.FindAllString(query, -1)
	if len(years) == 0 {
		return top
	}
	hitYear := strconv.Itoa(top.Published.Year())
	for _, y := range years {
		if y == hitYear {
			return top
		}
	}
	return nil
}

// ambiguousError reports that free text could not be resolved, listing
// every candidate found so that the agent can pick one and re-run fetch
// with an unambiguous identifier. No entry is created.
func (f *fetcher) ambiguousError(query string, hits []*sources.CrossrefWork) error {
	candidates := make([]sources.Candidate, 0, len(hits))
	for _, h := range hits {
		candidates = append(candidates, crossrefCandidate(h))
	}

	var notes []string
	zb := &sources.ZbMath{BaseURL: zbmathBase, Client: f.api}
	if found, err := zb.Search(query, searchRows); err != nil {
		notes = append(notes, "zbmath search failed: "+err.Error())
	} else {
		candidates = append(candidates, found...)
	}
	db := &sources.DBLP{BaseURL: dblpBase, Client: f.api}
	if found, err := db.Search(query, searchRows); err != nil {
		notes = append(notes, "dblp search failed: "+err.Error())
	} else {
		candidates = append(candidates, found...)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "fetch: cannot resolve %q to a single paper, so no entry was created.\n", query)
	if len(hits) == 0 {
		b.WriteString("Crossref found nothing for this query.\n")
	} else {
		b.WriteString("Crossref's top hit is not clearly better than the rest, or a year in the query contradicts it.\n")
	}

	if len(candidates) == 0 {
		b.WriteString("\nNo candidates were found in Crossref, zbMATH, or DBLP.\n")
	} else {
		b.WriteString("\nCandidates:\n")
		for i, c := range candidates {
			fmt.Fprintf(&b, "  [%d] %s: %s (%s)\n", i+1, c.Source, authorList(c.Authors), yearString(c.Year))
			fmt.Fprintf(&b, "      title: %s\n", c.Title)
			if c.Venue != "" {
				fmt.Fprintf(&b, "      in:    %s\n", c.Venue)
			}
			if c.DOI != "" {
				fmt.Fprintf(&b, "      doi:   https://doi.org/%s\n", c.DOI)
			} else {
				b.WriteString("      doi:   (none given)\n")
			}
		}
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "\n%s\n", n)
	}

	b.WriteString("\nPick the intended paper and re-run fetch with its identifier, e.g.\n")
	fmt.Fprintf(&b, "  paper fetch %s\n", exampleDOI(candidates))
	b.WriteString("  paper fetch arXiv:2412.05039\n")
	b.WriteString("If none of these is right, search the open web for the paper's DOI or arXiv ID first.")
	return errors.New(b.String())
}

// exampleDOI picks a DOI to show in the re-run instructions: the first
// candidate that has one, falling back to a well-known DOI when none of
// the candidates carries one.
func exampleDOI(candidates []sources.Candidate) string {
	for _, c := range candidates {
		if c.DOI != "" {
			return c.DOI
		}
	}
	return "10.1080/01621459.1963.10500830"
}

// crossrefCandidate converts a Crossref search hit into a Candidate, so
// that hits from all three services can be listed in one format.
func crossrefCandidate(w *sources.CrossrefWork) sources.Candidate {
	c := sources.Candidate{
		Source: "crossref",
		DOI:    w.DOI,
		Year:   w.Published.Year(),
	}
	if len(w.Titles) > 0 {
		c.Title = w.Titles[0]
	}
	if len(w.ContainerTitle) > 0 {
		c.Venue = w.ContainerTitle[0]
	}
	for _, a := range w.Authors {
		c.Authors = append(c.Authors, crossrefAuthorName(a))
	}
	return c
}

// crossrefAuthorName renders a Crossref author in natural order.
func crossrefAuthorName(a sources.CrossrefAuthor) string {
	if a.Given == "" {
		return a.Family
	}
	return a.Given + " " + a.Family
}

// authorList renders an author list for a human (or agent) reader.
func authorList(names []string) string {
	if len(names) == 0 {
		return "(authors unknown)"
	}
	return strings.Join(names, ", ")
}

// yearString renders a year for a human reader, marking an absent one.
func yearString(year int) string {
	if year == 0 {
		return "year unknown"
	}
	return strconv.Itoa(year)
}

// stopOnDuplicate implements behavior branch 5: it reports an entry that
// already holds this DOI or arXiv ID. The first return value says whether
// fetch must stop; the second is the error to return in that case (nil
// under -dry-run, where a duplicate is merely reported).
func (f *fetcher) stopOnDuplicate(what, doi, arxivID string) (bool, error) {
	papers, err := f.store.LoadAll()
	if err != nil {
		return true, fmt.Errorf("fetch: %w", err)
	}

	key := ""
	for _, p := range papers {
		if doi != "" && strings.EqualFold(p.DOI, doi) {
			key = p.Key
			break
		}
		if arxivID != "" && p.Arxiv != nil && p.Arxiv.ID == arxivID {
			key = p.Key
			break
		}
	}
	if key == "" {
		return false, nil
	}

	if f.dryRun {
		fmt.Printf("%s is already in the store as %s; nothing would be created\n", what, key)
		return true, nil
	}
	return true, fmt.Errorf("fetch: %s is already in the store as %s; use paper search %s to inspect it",
		what, key, key)
}

// create picks a free key for the draft entry, records why it exists, and
// writes it to the store.
func (f *fetcher) create(p *store.Paper, detail string) error {
	key, err := f.store.FreeKey(p.Key)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	p.Key = key
	p.AppendLog(f.now, "fetch", detail)
	if err := f.store.Save(p); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	fmt.Printf("created %s\n", p.Key)
	return nil
}

// attachDownload downloads url into a temporary file and hands that file
// to store.Attach, which moves it into the paper directory and records
// it. On a failed Attach the in-memory paper is stale — mutated but not
// saved — so it is discarded and reloaded from the store; the returned
// paper is always safe to save.
func (f *fetcher) attachDownload(p *store.Paper, url, filename, source string) (*store.Paper, error) {
	tmpDir, err := os.MkdirTemp("", "paper-fetch-")
	if err != nil {
		return p, err
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, filename)
	if err := resolve.FetchFile(f.dl, url, tmpPath, f.email); err != nil {
		return p, err
	}

	if err := f.store.Attach(p, tmpPath, filename, source, f.now); err != nil {
		reloaded, loadErr := f.store.Load(p.Key)
		if loadErr != nil {
			return nil, fmt.Errorf("%w (reloading %s afterwards also failed: %v)", err, p.Key, loadErr)
		}
		return reloaded, err
	}
	return p, nil
}

// unpaywallRoute asks Unpaywall for a directly downloadable open-access
// PDF. It returns the PDF URL, empty when there is none, together with a
// verdict spelling out what Unpaywall said. A failed lookup is a verdict,
// not a fatal error: the agent-fallback contract wants the entry created
// and the reason explained.
func (f *fetcher) unpaywallRoute(doi string) (pdfURL, verdict string) {
	u := &sources.Unpaywall{BaseURL: unpaywallBase, Client: f.api, Email: f.email}
	res, err := u.Lookup(doi)
	if err != nil {
		return "", "lookup failed: " + err.Error()
	}

	if res.BestOALocation == nil || res.BestOALocation.PDFURL == "" {
		if res.IsOA {
			return "", "is_oa=true, but no directly downloadable PDF URL is on record"
		}
		return "", "is_oa=false, no open-access location on record"
	}

	loc := res.BestOALocation
	return loc.PDFURL, fmt.Sprintf("is_oa=%t, best open-access location %s (host type %q)",
		res.IsOA, loc.PDFURL, loc.HostType)
}

// noOARouteError reports that the entry was created but no PDF could be
// downloaded, carrying everything fetch learned so the agent can hunt for
// the file without repeating the lookups.
func (f *fetcher) noOARouteError(p *store.Paper, work *sources.CrossrefWork, verdict string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "fetch: no open-access PDF route for %s.\n", p.Key)
	fmt.Fprintf(&b, "The draft entry was created in %s, but the PDF is missing.\n", f.store.Dir(p.Key))

	b.WriteString("\nWhat is known about the paper:\n")
	names := make([]string, 0, len(work.Authors))
	for _, a := range work.Authors {
		names = append(names, crossrefAuthorName(a))
	}
	fmt.Fprintf(&b, "  authors: %s\n", authorList(names))
	if len(work.Titles) > 0 {
		fmt.Fprintf(&b, "  title:   %s\n", work.Titles[0])
	}
	fmt.Fprintf(&b, "  year:    %s\n", yearString(work.Published.Year()))
	if len(work.ContainerTitle) > 0 {
		fmt.Fprintf(&b, "  in:      %s\n", work.ContainerTitle[0])
	}
	if work.Publisher != "" {
		fmt.Fprintf(&b, "  publisher: %s\n", work.Publisher)
	}
	fmt.Fprintf(&b, "  doi:     https://doi.org/%s\n", work.DOI)
	fmt.Fprintf(&b, "  unpaywall: %s\n", verdict)

	fmt.Fprintf(&b, "\nSearch the open web for a PDF (author home page, institutional repository,\n"+
		"preprint server, publisher page), save it as %s,\n"+
		"and record it under \"versions\" in %s.",
		filepath.Join(f.store.Dir(p.Key), "published.pdf"),
		filepath.Join(f.store.Dir(p.Key), "paper.json"))
	return errors.New(b.String())
}

// plannedDownload is one file or directory that a real run would fetch.
type plannedDownload struct {
	url  string
	name string // name inside the paper directory
}

// reportDryRun prints what a real run would create and download, without
// touching the store.
func (f *fetcher) reportDryRun(p *store.Paper, notes []string, downloads []plannedDownload) {
	key := p.Key
	if free, err := f.store.FreeKey(p.Key); err == nil {
		key = free
	}

	fmt.Printf("would create %s (@%s, status %s)\n", key, p.Bibtex.Type, p.Status)
	for _, field := range []string{"author", "title", "year", "journal", "doi", "eprint"} {
		if v := p.Bibtex.Fields[field]; v != "" {
			fmt.Printf("  %-8s %s\n", field+":", v)
		}
	}
	for _, n := range notes {
		fmt.Printf("  %s\n", n)
	}

	if len(downloads) == 0 {
		fmt.Println("would download nothing")
		return
	}
	for _, d := range downloads {
		fmt.Printf("would download %s -> %s\n", d.url, d.name)
	}
}

// arxivEprintID renders an arXiv ID with its version suffix, omitting the
// suffix when the version is unknown.
func arxivEprintID(id string, version int) string {
	if version <= 0 {
		return id
	}
	return fmt.Sprintf("%sv%d", id, version)
}

// arxivFileBase builds the store-side name for an arXiv download:
// "arxiv-2412.05039v2", used for the PDF (plus ".pdf") and for the
// directory holding the extracted tex source. Pre-2007 IDs contain a
// slash ("math.PR/0605234"), which cannot appear in a file name, so it
// becomes a hyphen here; the ID recorded in paper.json keeps the slash.
func arxivFileBase(id string, version int) string {
	return "arxiv-" + strings.ReplaceAll(arxivEprintID(id, version), "/", "-")
}

// arxivPDFURL is the download URL for an arXiv PDF. An unknown version
// falls back to the unversioned URL, which arXiv resolves to the latest.
func arxivPDFURL(id string, version int) string {
	if version <= 0 {
		return arxivDownloadBase + "/pdf/" + id
	}
	return sources.PDFURL(arxivDownloadBase, id, version)
}

// arxivSourceURL is the download URL for an arXiv e-print archive, with
// the same fallback as arxivPDFURL.
func arxivSourceURL(id string, version int) string {
	if version <= 0 {
		return arxivDownloadBase + "/e-print/" + id
	}
	return sources.SourceURL(arxivDownloadBase, id, version)
}

// addPending appends a note to a paper's Pending text, keeping whatever
// was already there.
func addPending(pending, note string) string {
	if pending == "" {
		return note
	}
	return pending + "; " + note
}
