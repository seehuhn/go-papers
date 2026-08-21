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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	s := testStore(t)
	c, err := s.LoadConfig()
	if err != nil || c.Email != "" {
		t.Fatalf("missing config.json: got (%v, %v), want empty config", c, err)
	}
	err = os.WriteFile(filepath.Join(s.Root, "config.json"),
		[]byte(`{"email": "test@example.org"}`+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.LoadConfig()
	if err != nil || c.Email != "test@example.org" {
		t.Fatalf("got (%v, %v), want email test@example.org", c, err)
	}
}
