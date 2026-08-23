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
	"strings"
)

// auditReport is the full result of a `paper audit` run: one auditEntry
// per reference in the .bib file, in file order.
type auditReport struct {
	Entries []auditEntry `json:"entries"`
}

// auditEntry is the verdict on one bibliography reference.
type auditEntry struct {
	Key  string `json:"key"` // the .bib citation key
	Line int    `json:"line"`
	// Existence is one of:
	//   confirmed  - matched a clean store entry, or an identifier resolved
	//   unverified - a source returned a plausible but unconfirmed candidate
	//   notFound   - no source returned any candidate; likely hallucinated
	//   unchecked  - not looked up (a source was down, so nothing was proved)
	Existence  string           `json:"existence"`
	StoreKey   string           `json:"storeKey,omitzero"`
	Holdings   string           `json:"holdings,omitzero"`
	Claims     int              `json:"claims,omitzero"` // recorded semantic-verification claims
	Problems   []string         `json:"problems,omitzero"`
	Candidates []auditCandidate `json:"candidates,omitzero"`
}

// auditCandidate is one source hit offered as a possible match for a
// reference that did not match the store.
type auditCandidate struct {
	Score   float64 `json:"score"`
	Title   string  `json:"title"`
	Authors string  `json:"authors"`
	Year    int     `json:"year"`
	DOI     string  `json:"doi,omitzero"`
	Source  string  `json:"source"` // "crossref"|"arxiv"|"zbmath"|"dblp"
}

// renderProse formats a report for a human (or an agent reading a
// terminal): a one-line summary, then a section per verdict, worst news
// first, so the entries that need attention are never buried below a
// long list of confirmed ones.
func renderProse(r *auditReport) string {
	var confirmed, unverified, notFound, unchecked []auditEntry
	for _, e := range r.Entries {
		switch e.Existence {
		case "confirmed":
			confirmed = append(confirmed, e)
		case "unverified":
			unverified = append(unverified, e)
		case "notFound":
			notFound = append(notFound, e)
		default:
			unchecked = append(unchecked, e)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d references: %d confirmed, %d unverified, %d not found, %d not checked\n",
		len(r.Entries), len(confirmed), len(unverified), len(notFound), len(unchecked))

	if len(notFound) > 0 {
		b.WriteString("\nNot found:\n")
		for _, e := range notFound {
			if e.StoreKey != "" {
				// Should be unreachable: run() clamps a store match's
				// notFound verdict to unverified before it ever gets here.
				// But the accusation must never render without the
				// counter-evidence, so print it anyway if it does.
				fmt.Fprintf(&b, "%s: no source returned any candidate, but held in the store as %s — not hallucinated\n", e.Key, e.StoreKey)
				continue
			}
			fmt.Fprintf(&b, "%s: no source returned any candidate — likely hallucinated\n", e.Key)
		}
	}

	if len(unverified) > 0 {
		b.WriteString("\nUnverified:\n")
		for _, e := range unverified {
			fmt.Fprintf(&b, "%s:\n", e.Key)
			for _, c := range e.Candidates {
				fmt.Fprintf(&b, "  %.2f  %s, %q, %d  [%s]\n", c.Score, c.Authors, c.Title, c.Year, c.Source)
			}
		}
	}

	var withProblems []auditEntry
	for _, e := range r.Entries {
		if len(e.Problems) > 0 {
			withProblems = append(withProblems, e)
		}
	}
	if len(withProblems) > 0 {
		b.WriteString("\nProblems:\n")
		for _, e := range withProblems {
			fmt.Fprintf(&b, "%s:\n", e.Key)
			for _, p := range e.Problems {
				fmt.Fprintf(&b, "  %s\n", p)
			}
		}
	}

	if len(confirmed) > 0 {
		names := make([]string, len(confirmed))
		for i, e := range confirmed {
			if e.StoreKey != "" {
				names[i] = fmt.Sprintf("%s -> %s", e.Key, e.StoreKey)
			} else {
				names[i] = e.Key
			}
			if e.Claims > 0 {
				noun := "claims"
				if e.Claims == 1 {
					noun = "claim"
				}
				names[i] += fmt.Sprintf(", %d %s recorded", e.Claims, noun)
			}
		}
		fmt.Fprintf(&b, "\nConfirmed: %s\n", strings.Join(names, ", "))
	}

	return b.String()
}

// renderJSON marshals r as indented JSON, followed by a trailing newline,
// matching the format `paper search -json` already uses.
func renderJSON(r *auditReport) (string, error) {
	data, err := json.Marshal(r, jsontext.WithIndent("  "))
	if err != nil {
		return "", fmt.Errorf("encoding JSON: %w", err)
	}
	return string(data) + "\n", nil
}
