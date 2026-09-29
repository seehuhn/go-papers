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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/sources"
	"seehuhn.de/go/paper/internal/store"
)

// newOpenAlex returns an OpenAlex client for one command run, using the
// configured API key and contact email.
func newOpenAlex(cfg *config.Config) *sources.OpenAlex {
	return &sources.OpenAlex{
		BaseURL: openAlexBase,
		Client:  &http.Client{Timeout: apiTimeout},
		Key:     cfg.OpenAlexKey,
		Email:   cfg.Email,
	}
}

// heldIndex finds the store paper that holds an OpenAlex work. Both maps
// are keyed by the lower-case identifier: OpenAlex gives DOIs in arbitrary
// case, and the store's spelling need not agree.
type heldIndex struct{ byDOI, byArxiv map[string]string }

// loadHeld indexes the DOIs and arXiv IDs of every paper in the store.
func loadHeld(s *store.Store) (*heldIndex, error) {
	papers, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	h := &heldIndex{byDOI: map[string]string{}, byArxiv: map[string]string{}}
	for _, p := range papers {
		if p.DOI != "" {
			h.byDOI[strings.ToLower(p.DOI)] = p.Key
		}
		if p.Arxiv != nil && p.Arxiv.ID != "" {
			h.byArxiv[strings.ToLower(p.Arxiv.ID)] = p.Key
		}
	}
	return h, nil
}

// key returns the key of the store paper that holds w, or "" if none does.
func (h *heldIndex) key(w *sources.OpenAlexWork) string {
	if w.DOI != "" {
		if k, ok := h.byDOI[strings.ToLower(w.DOI)]; ok {
			return k
		}
	}
	if w.ArxivID != "" {
		if k, ok := h.byArxiv[strings.ToLower(w.ArxivID)]; ok {
			return k
		}
	}
	return ""
}

// workLine is one work in a result list, with what the store says about it.
type workLine struct {
	Work         sources.OpenAlexWork
	Held         string // key of the store paper holding the work, or ""
	CitedByYours int    // related only
	CitesYours   int    // related only
}

// jsonWork is the JSON form of one workLine.
type jsonWork struct {
	ID           string   `json:"id"`
	DOI          string   `json:"doi,omitzero"`
	Arxiv        string   `json:"arxiv,omitzero"`
	Year         int      `json:"year"`
	Authors      []string `json:"authors"`
	Title        string   `json:"title"`
	Venue        string   `json:"venue,omitzero"`
	Type         string   `json:"type"`
	CitedByCount int      `json:"cited_by_count"`
	Abstract     string   `json:"abstract,omitzero"`
	Held         string   `json:"held,omitzero"`
	CitedByYours int      `json:"cited_by_yours,omitzero"`
	CitesYours   int      `json:"cites_yours,omitzero"`
}

// toJSONWork converts l to its JSON form. Authors is never nil, so that
// it encodes as a list.
func toJSONWork(l workLine) jsonWork {
	w := l.Work
	authors := w.Authors
	if authors == nil {
		authors = []string{}
	}
	return jsonWork{
		ID: w.ID, DOI: w.DOI, Arxiv: w.ArxivID, Year: w.Year,
		Authors: authors, Title: w.Title, Venue: w.Venue, Type: w.Type,
		CitedByCount: w.CitedByCount, Abstract: w.Abstract, Held: l.Held,
		CitedByYours: l.CitedByYours, CitesYours: l.CitesYours,
	}
}

// printWorks writes a result list to w: as one JSON object
// {"total": n, "works": [...]} if asJSON is set, and otherwise as a header
// line followed by one line per work and, unless short is set, an
// indented abstract line. short also drops the abstract from the JSON.
func printWorks(w io.Writer, total int, lines []workLine, short, asJSON bool) error {
	return printWorksOf(w, nil, total, lines, short, asJSON)
}

// printWorksOf is printWorks for a list that belongs to one work, head.
// A non-nil head is written first: as a line "work: <work line>" before
// the header line, or as the "work" member of the JSON object. It has no
// abstract line.
func printWorksOf(w io.Writer, head *workLine, total int, lines []workLine, short, asJSON bool) error {
	if asJSON {
		out := struct {
			Work  *jsonWork  `json:"work,omitzero"`
			Total int        `json:"total"`
			Works []jsonWork `json:"works"`
		}{Total: total, Works: make([]jsonWork, len(lines))}
		if head != nil {
			jw := toJSONWork(*head)
			if short {
				jw.Abstract = ""
			}
			out.Work = &jw
		}
		for i, l := range lines {
			out.Works[i] = toJSONWork(l)
			if short {
				out.Works[i].Abstract = ""
			}
		}
		data, err := json.Marshal(out, jsontext.WithIndent("  "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	}

	if head != nil {
		fmt.Fprintf(w, "work: %s\n", workText(*head))
	}
	fmt.Fprintf(w, "total: %d, shown: %d\n", total, len(lines))
	for _, l := range lines {
		fmt.Fprintln(w, workText(l))
		if short {
			continue
		}
		abstract := l.Work.Abstract
		if abstract == "" {
			abstract = "none"
		}
		fmt.Fprintf(w, "    abstract: %s\n", abstract)
	}
	return nil
}

// workText formats the one-line text form of l.
func workText(l workLine) string {
	w := l.Work
	year := "?"
	if w.Year != 0 {
		year = fmt.Sprint(w.Year)
	}
	surname := "?"
	if len(w.Authors) > 0 {
		if f := strings.Fields(w.Authors[0]); len(f) > 0 {
			surname = f[len(f)-1]
		}
	}
	fields := []string{w.ID, year, surname, w.Title, w.Venue, fmt.Sprintf("cited %d", w.CitedByCount)}
	if l.CitedByYours != 0 {
		fields = append(fields, fmt.Sprintf("cited-by-yours %d", l.CitedByYours))
	}
	if l.CitesYours != 0 {
		fields = append(fields, fmt.Sprintf("cites-yours %d", l.CitesYours))
	}
	if w.DOI != "" {
		fields = append(fields, "doi:"+w.DOI)
	}
	if w.ArxivID != "" {
		fields = append(fields, "arxiv:"+w.ArxivID)
	}
	if l.Held != "" {
		fields = append(fields, "held:"+l.Held)
	}
	return strings.Join(fields, " | ")
}

// logOpenAlex records one OpenAlex command in the event log.
func logOpenAlex(s *store.Store, cmd, ref, outcome string, hits int, d time.Duration) {
	s.LogEvent(store.Event{
		Command:  cmd,
		Ref:      ref,
		Source:   "openalex",
		Outcome:  outcome,
		Hits:     hits,
		Duration: d.Milliseconds(),
	})
}

// logOpenAlexErr records a failed OpenAlex command in the event log. The
// outcome is that of err (see eventOutcome) and the detail its first line.
func logOpenAlexErr(s *store.Store, cmd, ref string, err error, d time.Duration) {
	s.LogEvent(store.Event{
		Command:  cmd,
		Ref:      ref,
		Source:   "openalex",
		Outcome:  eventOutcome(err),
		Detail:   eventDetail(err),
		Duration: d.Milliseconds(),
	})
}

// checkNoTrailingFlags returns an error naming the first of args that looks
// like a flag.  The flag package stops at the first operand, so a flag after
// it would otherwise be read as part of the operand: noun says what the
// operand is.
func checkNoTrailingFlags(cmd, noun string, args []string) error {
	for _, a := range args {
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			return fmt.Errorf("%s: flags go before the %s (got %q)", cmd, noun, a)
		}
	}
	return nil
}
