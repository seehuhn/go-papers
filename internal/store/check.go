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

package store

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/doi"
	"seehuhn.de/go/paper/internal/tex"
)

// Problem describes one issue found by CheckPaper. Severity is either
// "error" (the entry is broken or inconsistent) or "warning" (the entry
// is usable but worth a human's attention).
type Problem struct {
	Key      string
	Severity string
	Msg      string
}

// arxivNewPattern matches the post-2007 arXiv identifier style, e.g.
// "0706.0001".
var arxivNewPattern = regexp.MustCompile(`^\d{4}\.\d{4,5}$`)

// arxivOldPattern matches the pre-2007 arXiv identifier style, e.g.
// "math.PR/0611001" or "hep-th/9901001".
var arxivOldPattern = regexp.MustCompile(`^[a-z-]+(\.[A-Z]{2})?/\d{7}$`)

// pageRangeSingleDash matches a single "-" between two digits, the
// pattern used by mistake where a "--" range separator was intended.
var pageRangeSingleDash = regexp.MustCompile(`\d-\d`)

// validStatuses and validHoldings enumerate the only legal values for
// Paper.Status and Paper.Holdings.
var validStatuses = map[string]bool{"draft": true, "clean": true}
var validHoldings = map[string]bool{"none": true, "preprint": true, "published": true, "both": true}

// ValidStatus reports whether s is a legal value for Paper.Status
// ("draft" or "clean").
func ValidStatus(s string) bool {
	return validStatuses[s]
}

// ValidHoldings reports whether s is a legal value for Paper.Holdings
// ("none", "preprint", "published", or "both").
func ValidHoldings(s string) bool {
	return validHoldings[s]
}

// CheckPaper runs all offline, per-entry validation rules against p and
// returns every problem found. A nil or empty result means p is clean.
// CheckPaper never touches the network or the filesystem; it only
// inspects the fields of p.
func CheckPaper(p *Paper) []Problem {
	var problems []Problem

	problems = append(problems, checkEntryType(p)...)
	problems = append(problems, checkRequiredFields(p)...)
	problems = append(problems, checkNames(p)...)
	problems = append(problems, checkFieldEncoding(p)...)
	problems = append(problems, checkRawSpecials(p)...)
	problems = append(problems, checkDOI(p)...)
	problems = append(problems, checkArxiv(p)...)
	problems = append(problems, checkStatusHoldings(p)...)
	problems = append(problems, checkPages(p)...)
	problems = append(problems, checkTitleCapitalization(p)...)
	problems = append(problems, checkArticleHasDOI(p)...)
	problems = append(problems, checkDraftStatus(p)...)
	problems = append(problems, checkConsistency(p)...)
	problems = append(problems, checkAudit(p)...)

	return problems
}

// sortedFieldNames returns the names of p.Bibtex.Fields in sorted order,
// so that field-by-field checks below produce deterministic output.
func sortedFieldNames(p *Paper) []string {
	names := make([]string, 0, len(p.Bibtex.Fields))
	for name := range p.Bibtex.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Rule 1: Bibtex.Type not in KnownTypes.
func checkEntryType(p *Paper) []Problem {
	for _, t := range bibtex.KnownTypes {
		if p.Bibtex.Type == t {
			return nil
		}
	}
	return []Problem{{p.Key, "error", fmt.Sprintf(
		"bibtex.type: unknown entry type %q (known types: %s)",
		p.Bibtex.Type, strings.Join(bibtex.KnownTypes, ", "))}}
}

// Rule 2: a RequiredFields group has no present field.
func checkRequiredFields(p *Paper) []Problem {
	groups, ok := bibtex.RequiredFields[p.Bibtex.Type]
	if !ok {
		// Unknown type: already reported by checkEntryType; nothing more
		// to say about required fields.
		return nil
	}

	var problems []Problem
	for _, group := range groups {
		present := false
		for _, field := range group {
			if p.Bibtex.Fields[field] != "" {
				present = true
				break
			}
		}
		if !present {
			problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
				"bibtex.fields: missing required field(s) %s",
				strings.Join(group, " or "))})
		}
	}
	return problems
}

// Rule 3: author/editor present but bibtex.ParseNames fails.
func checkNames(p *Paper) []Problem {
	var problems []Problem
	for _, field := range []string{"author", "editor"} {
		value, ok := p.Bibtex.Fields[field]
		if !ok || value == "" {
			continue
		}
		if _, err := bibtex.ParseNames(value); err != nil {
			problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
				"bibtex.fields.%s: cannot parse name list %q: %v",
				field, value, err)})
		}
	}
	return problems
}

// Rule 4: any field where tex.Decode reports unknown macros.
// Rule 5: unbalanced braces in any field.
func checkFieldEncoding(p *Paper) []Problem {
	var problems []Problem
	for _, field := range sortedFieldNames(p) {
		value := p.Bibtex.Fields[field]

		if !bracesBalanced(value) {
			problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
				"bibtex.fields.%s: unbalanced braces in %q", field, value)})
		}

		_, unknown := tex.Decode(value)
		for _, name := range unknown {
			problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
				`bibtex.fields.%s: unknown macro \%s in %q`, field, name, value)})
		}
	}
	return problems
}

// Rule 5a: a field containing a raw, unescaped "&" or "%". Both are TeX
// syntax rather than text — "&" is an alignment tab, "%" starts a
// comment — so a field carrying one verbatim typesets wrongly or
// swallows the rest of the line. This is a backstop for metadata that
// did not come through tex.Encode (hand edits, older entries), hence a
// warning: the entry is readable, but the bibtex it generates is not
// what the author meant.
func checkRawSpecials(p *Paper) []Problem {
	var problems []Problem
	for _, field := range sortedFieldNames(p) {
		value := p.Bibtex.Fields[field]
		for _, ch := range rawSpecials(value) {
			problems = append(problems, Problem{p.Key, "warning", fmt.Sprintf(
				`bibtex.fields.%s: raw %q in %q should be escaped as \%s`,
				field, ch, value, ch)})
		}
	}
	return problems
}

// rawSpecials returns, in order of first appearance and without
// repetition, the special characters ("&", "%") that appear in s
// unescaped and outside math mode. A backslash escapes the character
// following it, and a matched pair of "$" delimits math mode, whose
// content is skipped exactly as tex.Decode skips it; an unmatched "$" is
// an ordinary character.
func rawSpecials(s string) []string {
	var found []string
	seen := make(map[byte]bool)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // whatever follows the backslash is escaped
		case '$':
			if j := strings.IndexByte(s[i+1:], '$'); j >= 0 {
				i += j + 1
			}
		case '&', '%':
			if !seen[s[i]] {
				seen[s[i]] = true
				found = append(found, string(s[i]))
			}
		}
	}
	return found
}

// bracesBalanced reports whether s has properly nested and matched
// curly braces.
func bracesBalanced(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// Rule 6: DOI set but not syntactically well-formed (see doi.Syntactic).
func checkDOI(p *Paper) []Problem {
	if p.DOI == "" || doi.Syntactic(p.DOI) {
		return nil
	}
	return []Problem{{p.Key, "error", fmt.Sprintf(
		"doi: %q does not look like a valid DOI (expected form 10.NNNN/suffix)", p.DOI)}}
}

// Rule 7: Arxiv.ID set but matching neither the new nor the old arXiv id
// style.
func checkArxiv(p *Paper) []Problem {
	if p.Arxiv == nil || p.Arxiv.ID == "" {
		return nil
	}
	id := p.Arxiv.ID
	if arxivNewPattern.MatchString(id) || arxivOldPattern.MatchString(id) {
		return nil
	}
	return []Problem{{p.Key, "error", fmt.Sprintf(
		"arxiv.id: %q does not look like a valid arXiv identifier", id)}}
}

// Rule 8: Status not in {draft, clean}, or Holdings not in {none,
// preprint, published, both}.
func checkStatusHoldings(p *Paper) []Problem {
	var problems []Problem
	if !validStatuses[p.Status] {
		problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
			"status: unknown status %q (want one of draft, clean)", p.Status)})
	}
	if !validHoldings[p.Holdings] {
		problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
			"holdings: unknown holdings %q (want one of none, preprint, published, both)", p.Holdings)})
	}
	return problems
}

// Rule 9: pages contains a single "-" between digits (should be "--").
func checkPages(p *Paper) []Problem {
	pages, ok := p.Bibtex.Fields["pages"]
	if !ok || pages == "" {
		return nil
	}
	if pageRangeSingleDash.MatchString(pages) {
		return []Problem{{p.Key, "warning", fmt.Sprintf(
			`bibtex.fields.pages: %q uses a single "-"; page ranges should use "--"`, pages)}}
	}
	return nil
}

// Rule 10: title has a capitalized word outside braces, beyond position
// one. Tokenization runs on the raw (still bibtex-encoded) title: words
// are split on whitespace at brace level 0, and any text inside a brace
// group is protected (excluded from the capitalization check), since it
// is understood to be deliberately case-locked by the author.
func checkTitleCapitalization(p *Paper) []Problem {
	title, ok := p.Bibtex.Fields["title"]
	if !ok || title == "" {
		return nil
	}

	type word struct {
		raw     string // the word as it appears in the source, braces included
		visible string // the word with any brace-protected text removed
	}
	var words []word
	var raw, visible strings.Builder
	depth := 0
	flush := func() {
		if raw.Len() > 0 {
			words = append(words, word{raw: raw.String(), visible: visible.String()})
			raw.Reset()
			visible.Reset()
		}
	}
	for _, r := range title {
		switch {
		case r == '{':
			depth++
			raw.WriteRune(r)
		case r == '}':
			if depth > 0 {
				depth--
			}
			raw.WriteRune(r)
		case unicode.IsSpace(r) && depth == 0:
			flush()
		default:
			raw.WriteRune(r)
			if depth == 0 {
				visible.WriteRune(r)
			}
		}
	}
	flush()

	var problems []Problem
	for i, w := range words {
		if i == 0 || w.visible == "" {
			continue
		}
		hasUpper := false
		for _, r := range w.visible {
			if unicode.IsUpper(r) {
				hasUpper = true
				break
			}
		}
		if hasUpper {
			problems = append(problems, Problem{p.Key, "warning", fmt.Sprintf(
				"bibtex.fields.title: word %q may need brace protection", w.raw)})
		}
	}
	return problems
}

// Rule 11: type article but no doi field and DOI empty.
func checkArticleHasDOI(p *Paper) []Problem {
	if p.Bibtex.Type != "article" {
		return nil
	}
	if p.Bibtex.Fields["doi"] != "" || p.DOI != "" {
		return nil
	}
	return []Problem{{p.Key, "warning",
		"doi: article entry has no bibtex.fields.doi and no top-level DOI"}}
}

// Rule 12: Status == "draft" (drafts are legal but worth listing).
func checkDraftStatus(p *Paper) []Problem {
	if p.Status != "draft" {
		return nil
	}
	return []Problem{{p.Key, "warning", "status: paper is still in draft status"}}
}

// Rule 13: bibtex.fields.doi/eprint inconsistent with top-level
// DOI/Arxiv when both are set.
func checkConsistency(p *Paper) []Problem {
	var problems []Problem

	if fieldDOI := p.Bibtex.Fields["doi"]; fieldDOI != "" && p.DOI != "" && fieldDOI != p.DOI {
		problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
			"doi: bibtex.fields.doi %q does not match top-level DOI %q", fieldDOI, p.DOI)})
	}

	if fieldEprint := p.Bibtex.Fields["eprint"]; fieldEprint != "" && p.Arxiv != nil && p.Arxiv.ID != "" && fieldEprint != p.Arxiv.ID {
		problems = append(problems, Problem{p.Key, "error", fmt.Sprintf(
			"arxiv: bibtex.fields.eprint %q does not match top-level Arxiv.ID %q", fieldEprint, p.Arxiv.ID)})
	}

	return problems
}

// validVerdicts enumerates the only legal values for Claim.Verdict.
var validVerdicts = map[string]bool{
	"supports": true, "partial": true, "refutes": true, "unverifiable": true,
}

// isoDate matches an ISO calendar date, the form used everywhere in the
// store.
var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Rule 14: checkAudit validates the semantic-verification records. A
// verdict is a claim about what a paper says, so a record that does not
// name what was checked, against which held file, on what date, is not a
// record.
func checkAudit(p *Paper) []Problem {
	if p.Audit == nil {
		return nil
	}
	var out []Problem
	bad := func(format string, args ...any) {
		out = append(out, Problem{Key: p.Key, Severity: "error",
			Msg: "audit: " + fmt.Sprintf(format, args...)})
	}
	for i, c := range p.Audit.Claims {
		if strings.TrimSpace(c.Claim) == "" {
			bad("claim %d has an empty claim", i+1)
		}
		if !validVerdicts[c.Verdict] {
			bad("claim %d has verdict %q; want supports, partial, refutes or unverifiable", i+1, c.Verdict)
		}
		if !isoDate.MatchString(c.Date) {
			bad("claim %d has date %q; want YYYY-MM-DD", i+1, c.Date)
		}
		if c.Version == "" {
			bad("claim %d does not say which version was checked", i+1)
		} else if _, held := p.Versions[c.Version]; !held {
			bad("claim %d was checked against %q, which this entry does not hold", i+1, c.Version)
		}
	}
	return out
}
