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

// Parse reads a bibtex file. Values are returned bibtex-encoded, exactly
// as written in the file minus the outer delimiters, matching the
// store's convention that field values are never decoded.
func Parse(r io.Reader) ([]KeyedEntry, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading bibtex: %w", err)
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
func (p *parser) file() ([]KeyedEntry, error) {
	var entries []KeyedEntry
	for p.skipToAt() {
		e, err := p.entry()
		if err != nil {
			return nil, err
		}
		if e != nil {
			entries = append(entries, *e)
		}
	}
	if err := inheritCrossrefs(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// inheritCrossrefs is a post-pass over all parsed entries: for each
// entry with a non-empty "crossref" field, it copies every field the
// child lacks from the referenced parent entry (the child's own fields
// always win). This runs only after the whole file has been parsed,
// because bibtex allows a parent entry to appear after its child.
// Crossref chains deeper than one level are out of scope, matching
// bibtex itself, which only guarantees one level. The "crossref" field
// is left on the child.
func inheritCrossrefs(entries []KeyedEntry) error {
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
			return fmt.Errorf("bibtex: line %d: entry %q crossrefs missing entry %q", child.Line, child.Key, parentKey)
		}
		parent := entries[parentIdx]
		for name, value := range parent.Entry.Fields {
			if _, exists := child.Entry.Fields[name]; !exists {
				child.Entry.Fields[name] = value
			}
		}
	}
	return nil
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
func (p *parser) entry() (*KeyedEntry, error) {
	startLine := p.line
	p.next() // consume '@'
	typ := strings.ToLower(p.readName())
	p.skipSpace()
	if p.peek() != '{' {
		return nil, fmt.Errorf("bibtex: line %d: expected '{' after @%s", p.line, typ)
	}
	p.next() // consume '{'

	switch typ {
	case "string":
		return nil, p.stringMacro()
	case "comment", "preamble":
		return nil, p.skipBraced(startLine)
	}

	p.skipSpace()
	key := p.readKey()
	p.skipSpace()

	fields := make(map[string]string)
	for {
		p.skipSpace()
		if p.peek() == '}' {
			p.next()
			break
		}
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("bibtex: line %d: unterminated entry %q", startLine, key)
		}
		if p.peek() == ',' {
			p.next()
			continue
		}
		name := strings.ToLower(p.readName())
		if name == "" {
			return nil, fmt.Errorf("bibtex: line %d: expected field name in entry %q", p.line, key)
		}
		p.skipSpace()
		if p.peek() != '=' {
			return nil, fmt.Errorf("bibtex: line %d: expected '=' after field %q", p.line, name)
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
func (p *parser) value() (string, error) {
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
func (p *parser) oneValue() (string, error) {
	startLine := p.line
	switch c := p.peek(); {
	case c == '{':
		p.next()
		start := p.pos
		depth := 1
		for depth > 0 {
			if p.pos >= len(p.src) {
				return "", fmt.Errorf("bibtex: line %d: unterminated brace group", startLine)
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
				return "", fmt.Errorf("bibtex: line %d: unterminated quoted value", startLine)
			}
			c := p.next()
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
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
			return "", fmt.Errorf("bibtex: line %d: undefined macro %q", startLine, name)
		}
		return v, nil

	default:
		return "", fmt.Errorf("bibtex: line %d: unexpected character %q in value", startLine, c)
	}
}

// stringMacro parses an "@string{name = value}" body, having already
// consumed the opening '{', and records name -> value (bibtex-encoded)
// into p.macro. The name is lowercased so lookups are case-insensitive.
func (p *parser) stringMacro() error {
	p.skipSpace()
	name := strings.ToLower(p.readName())
	p.skipSpace()
	if p.peek() != '=' {
		return fmt.Errorf("bibtex: line %d: expected '=' in @string definition", p.line)
	}
	p.next() // consume '='
	p.skipSpace()
	value, err := p.value()
	if err != nil {
		return err
	}
	p.macro[name] = value
	p.skipSpace()
	if p.peek() == '}' {
		p.next()
	}
	return nil
}

// skipBraced consumes one balanced-brace group, having already consumed
// its opening '{'. Used for @comment and @preamble bodies, whose
// contents are discarded. startLine is the line the construct began on,
// for the unterminated-group error.
func (p *parser) skipBraced(startLine int) error {
	depth := 1
	for depth > 0 {
		if p.pos >= len(p.src) {
			return fmt.Errorf("bibtex: line %d: unterminated brace group", startLine)
		}
		c := p.next()
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
		}
	}
	return nil
}
