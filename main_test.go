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

import "testing"

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
