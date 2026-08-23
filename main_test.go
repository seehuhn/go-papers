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
	"strings"
	"testing"
)

func TestDispatchUnknown(t *testing.T) {
	err := dispatch([]string{"no-such-command"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestDispatchHelp(t *testing.T) {
	if err := dispatch([]string{"help"}); err != nil {
		t.Fatalf("help failed: %v", err)
	}
}

func TestDispatchEmpty(t *testing.T) {
	if err := dispatch(nil); err != nil {
		t.Fatalf("dispatch with nil args failed: %v", err)
	}
}

func TestHelpFlagExitsZero(t *testing.T) {
	// dispatch returns flag.ErrHelp-derived nil for -h at the command level
	err := dispatch([]string{"bib", "-h"})
	if err != nil {
		t.Errorf("bib -h: got error %v, want nil", err)
	}
	// top-level -h / -help / --help behave like "paper help"
	for _, arg := range []string{"-h", "-help", "--help"} {
		if err := dispatch([]string{arg}); err != nil {
			t.Errorf("paper %s: got error %v, want nil", arg, err)
		}
	}
}

func TestErrorPrefixNotDoubled(t *testing.T) {
	noConfig(t)
	err := dispatch([]string{"bib", "-all"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.HasPrefix(err.Error(), "paper ") {
		t.Errorf("error %q must not start with 'paper ' (main.go adds that prefix)", err)
	}
	if !strings.HasPrefix(err.Error(), "bib: ") {
		t.Errorf("error %q should start with the bare command name 'bib: '", err)
	}
}
