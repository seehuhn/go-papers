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

package bibtex

import (
	"fmt"
	"io"
	"strings"
)

// KeyedEntry is one entry from a .bib file: its citation key, its
// contents, and the line the entry started on.
type KeyedEntry struct {
	Key   string
	Entry Entry
	Line  int
}

// parser holds the state of a hand-rolled scan over a bibtex source
// buffer: a byte offset and the current line number (for error
// messages).
type parser struct {
	src   []byte
	pos   int
	line  int
	macro map[string]string // @string definitions, lowercased names
}

// ParseError describes one entry Parse could not read. The entry is
// skipped and parsing continues, so a single typo does not make a whole
// bibliography unreadable; Line points at where the failure was noticed.
type ParseError struct {
	Line int
	Msg  string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("bibtex: line %d: %s", e.Line, e.Msg)
}

// Parse reads a bibtex file. Values are returned bibtex-encoded, exactly
// as written in the file minus the outer delimiters, matching the
// store's convention that field values are never decoded.
//
// Parse recovers from malformed entries: each failure is reported as a
// ParseError and scanning resumes at the next '@', so the well-formed
// entries are always returned. A read failure on r is reported as a
// ParseError with Line 0.
//
// Scope: both delimiter styles (@type{...} and @type(...)) are read;
// the twelve month macros are predefined and @string may redefine them
// (last definition wins, as in bibtex); crossref inheritance is one
// level deep, matching what bibtex itself guarantees. The input is
// expected to be roughly what bibtex would accept - this is a reader
// for real bibliographies, not a validator.
func Parse(r io.Reader) ([]KeyedEntry, []ParseError) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, []ParseError{{Line: 0, Msg: fmt.Sprintf("reading bibtex: %v", err)}}
	}
	p := &parser{src: src, line: 1, macro: builtinMonthMacros()}
	return p.file()
}

// builtinMonthMacros returns the twelve three-letter month macros bibtex
// predefines (jan...dec), expanded to the conventional month name that
// bibtex styles render. A file's own @string definitions are applied
// after this map is seeded, so a user redefinition of e.g. "mar" wins,
// matching bibtex's last-definition-wins rule.
func builtinMonthMacros() map[string]string {
	return map[string]string{
		"jan": "January", "feb": "February", "mar": "March", "apr": "April",
		"may": "May", "jun": "June", "jul": "July", "aug": "August",
		"sep": "September", "oct": "October", "nov": "November", "dec": "December",
	}
}

// entryFail is the internal error the scanning methods return: a line
// number and a message, which file() turns into a ParseError. Keeping
// the two apart lets Error() carry the "bibtex: line N:" prefix exactly
// once.
type entryFail struct {
	line int
	msg  string
}

func (e *entryFail) Error() string {
	return fmt.Sprintf("bibtex: line %d: %s", e.line, e.msg)
}

// failf builds an entryFail at the given line.
func failf(line int, format string, args ...any) *entryFail {
	return &entryFail{line: line, msg: fmt.Sprintf(format, args...)}
}

// next returns the current byte and advances past it, tracking the line
// number. Every advance over p.src must go through next, or the line
// numbers drift.
func (p *parser) next() byte {
	c := p.src[p.pos]
	p.pos++
	if c == '\n' {
		p.line++
	}
	return c
}

// peek returns the current byte without advancing, or 0 at end of input.
func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// skipSpace advances past whitespace.
func (p *parser) skipSpace() {
	for p.pos < len(p.src) && isSpace(p.src[p.pos]) {
		p.next()
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// file parses the whole source: everything outside an "@type{...}"
// entry is a bibtex comment, so file skips to the next '@' rather than
// erroring on it.
func (p *parser) file() ([]KeyedEntry, []ParseError) {
	var entries []KeyedEntry
	var errs []ParseError
	for p.skipToAt() {
		e, fail := p.entry()
		if fail != nil {
			// Recovery: report the entry, then resume at the next '@'.
			// One typo must not make a whole bibliography unreadable.
			errs = append(errs, ParseError{Line: fail.line, Msg: fail.msg})
			p.resync()
			continue
		}
		if e != nil {
			entries = append(entries, *e)
		}
	}
	errs = append(errs, inheritCrossrefs(entries)...)
	return entries, errs
}

// resync advances to the next '@', where the next entry can plausibly
// start. Best effort: an '@' inside a broken entry's remaining text
// resumes parsing early, which at worst reports one extra error. The
// failure position may already be on the next entry's '@' and must not
// be skipped.
func (p *parser) resync() {
	p.skipToAt()
}

// inheritCrossrefs is a post-pass over all parsed entries: for each
// entry with a non-empty "crossref" field, it copies every field the
// child lacks from the referenced parent entry (the child's own fields
// always win). This runs only after the whole file has been parsed,
// because bibtex allows a parent entry to appear after its child.
// Crossref chains deeper than one level are out of scope, matching
// bibtex itself, which only guarantees one level. The "crossref" field
// is left on the child.
func inheritCrossrefs(entries []KeyedEntry) []ParseError {
	var errs []ParseError
	byKey := make(map[string]int, len(entries))
	for i, e := range entries {
		byKey[e.Key] = i
	}
	for i := range entries {
		child := &entries[i]
		parentKey, ok := child.Entry.Fields["crossref"]
		if !ok || parentKey == "" {
			continue
		}
		parentIdx, ok := byKey[parentKey]
		if !ok {
			// The child's own fields are still worth auditing, so it is
			// kept; only the inheritance is reported as missing.
			errs = append(errs, ParseError{Line: child.Line,
				Msg: fmt.Sprintf("entry %q crossrefs missing entry %q", child.Key, parentKey)})
			continue
		}
		parent := entries[parentIdx]
		for name, value := range parent.Entry.Fields {
			if _, exists := child.Entry.Fields[name]; !exists {
				child.Entry.Fields[name] = value
			}
		}
	}
	return errs
}

// skipToAt advances to the next '@', reporting whether one was found.
func (p *parser) skipToAt() bool {
	for p.pos < len(p.src) {
		if p.src[p.pos] == '@' {
			return true
		}
		p.next()
	}
	return false
}

// entry parses one "@type{...}" construct starting at the '@'. It
// returns nil, nil for constructs that produce no KeyedEntry: @string
// records a macro definition, @comment and @preamble are discarded.
func (p *parser) entry() (*KeyedEntry, *entryFail) {
	startLine := p.line
	p.next() // consume '@'
	typ := strings.ToLower(p.readName())
	p.skipSpace()
	// Both delimiter styles bibtex accepts: @type{...} and @type(...).
	// The closer is fixed by the opener; a '}' cannot end a paren entry.
	var closer byte
	switch p.peek() {
	case '{':
		closer = '}'
	case '(':
		closer = ')'
	default:
		return nil, failf(p.line, "expected '{' or '(' after @%s", typ)
	}
	p.next() // consume the opener

	switch typ {
	case "string":
		return nil, p.stringMacro(closer)
	case "comment", "preamble":
		opener := byte('{')
		if closer == ')' {
			opener = '('
		}
		return nil, p.skipBraced(startLine, opener, closer)
	}

	p.skipSpace()
	key := p.readKey()
	p.skipSpace()

	fields := make(map[string]string)
	for {
		p.skipSpace()
		if p.peek() == closer {
			p.next()
			break
		}
		if p.pos >= len(p.src) {
			return nil, failf(startLine, "unterminated entry %q", key)
		}
		if p.peek() == ',' {
			p.next()
			continue
		}
		name := strings.ToLower(p.readName())
		if name == "" {
			return nil, failf(p.line, "expected field name in entry %q", key)
		}
		p.skipSpace()
		if p.peek() != '=' {
			return nil, failf(p.line, "expected '=' after field %q", name)
		}
		p.next() // consume '='
		p.skipSpace()
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		fields[name] = value
	}

	return &KeyedEntry{
		Key:   key,
		Entry: Entry{Type: typ, Fields: fields},
		Line:  startLine,
	}, nil
}

// readName reads a run of letters, digits, '_' and '-' - a bibtex
// identifier (entry type or field name).
func (p *parser) readName() string {
	start := p.pos
	for p.pos < len(p.src) && isNameByte(p.src[p.pos]) {
		p.next()
	}
	return string(p.src[start:p.pos])
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// readKey reads a citation key: everything up to the first ',' or '}'.
func (p *parser) readKey() string {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ',' && p.src[p.pos] != '}' {
		p.next()
	}
	return strings.TrimSpace(string(p.src[start:p.pos]))
}

// value parses one field value, including any "#"-concatenated
// continuations: after reading one value, if the next non-space byte is
// '#', it is consumed and the next value is appended.
func (p *parser) value() (string, *entryFail) {
	var parts []string
	for {
		p.skipSpace()
		part, err := p.oneValue()
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
		p.skipSpace()
		if p.peek() != '#' {
			break
		}
		p.next() // consume '#'
	}
	return strings.Join(parts, ""), nil
}

// oneValue parses a single value token, dispatching on the first
// non-space byte: '{' for a balanced-brace group (nested braces counted
// so that "{Vo{\ss}, Jochen}" comes back as "Vo{\ss}, Jochen" - the
// outer delimiter is stripped and nothing else), '"' for a quoted
// string (interior braces and quotes survive; a '"' inside a brace
// group does not end the value), a digit for a bare number, or a letter
// for a macro name, resolved against p.macro (lowercased); an undefined
// macro is an error naming the macro and the line. It does not handle
// "#" concatenation - see value.
func (p *parser) oneValue() (string, *entryFail) {
	startLine := p.line
	switch c := p.peek(); {
	case c == '{':
		p.next()
		start := p.pos
		depth := 1
		for depth > 0 {
			if p.pos >= len(p.src) {
				return "", failf(startLine, "unterminated brace group")
			}
			c := p.next()
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
			}
		}
		return string(p.src[start : p.pos-1]), nil

	case c == '"':
		p.next()
		start := p.pos
		depth := 0
		for {
			if p.pos >= len(p.src) {
				return "", failf(startLine, "unterminated quoted value")
			}
			c := p.next()
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth < 0 {
					// Real bibtex rejects this as unbalanced braces.
					// Erroring here keeps the report pointing at the
					// fault instead of at wherever the scan finally
					// runs out of input.
					return "", failf(p.line, "unbalanced '}' in quoted value")
				}
			} else if c == '"' && depth == 0 {
				return string(p.src[start : p.pos-1]), nil
			}
		}

	case c >= '0' && c <= '9':
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.next()
		}
		return string(p.src[start:p.pos]), nil

	case isNameByte(c):
		name := strings.ToLower(p.readName())
		v, ok := p.macro[name]
		if !ok {
			return "", failf(startLine, "undefined macro %q", name)
		}
		return v, nil

	default:
		return "", failf(startLine, "unexpected character %q in value", c)
	}
}

// stringMacro parses an "@string{name = value}" body, having already
// consumed the opening '{', and records name -> value (bibtex-encoded)
// into p.macro. The name is lowercased so lookups are case-insensitive.
func (p *parser) stringMacro(closer byte) *entryFail {
	p.skipSpace()
	name := strings.ToLower(p.readName())
	p.skipSpace()
	if p.peek() != '=' {
		return failf(p.line, "expected '=' in @string definition")
	}
	p.next() // consume '='
	p.skipSpace()
	value, err := p.value()
	if err != nil {
		return err
	}
	p.macro[name] = value
	p.skipSpace()
	if p.peek() == closer {
		p.next()
	}
	return nil
}

// skipBraced consumes one balanced-brace group, having already consumed
// its opening '{'. Used for @comment and @preamble bodies, whose
// contents are discarded. startLine is the line the construct began on,
// for the unterminated-group error.
func (p *parser) skipBraced(startLine int, opener, closer byte) *entryFail {
	depth := 1
	for depth > 0 {
		if p.pos >= len(p.src) {
			kind := "brace"
			if closer == ')' {
				kind = "parenthesis"
			}
			return failf(startLine, "unterminated %s group", kind)
		}
		c := p.next()
		if c == opener {
			depth++
		} else if c == closer {
			depth--
		}
	}
	return nil
}
