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

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathDefaultsToDotPaperJSONInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvVar, "")

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(home, ".paper.json")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestPathHonoursTheEnvironmentVariable(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "other.json")
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvVar, elsewhere)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != elsewhere {
		t.Errorf("Path = %q, want %q", got, elsewhere)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := &Config{Store: "/papers", Email: "voss@example.org"}

	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Store != want.Store {
		t.Errorf("Store = %q, want %q", got.Store, want.Store)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
}

func TestSaveKeepsTheConfigPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := (&Config{Store: "/papers"}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestSaveCreatesTheParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	if err := (&Config{Store: "/papers"}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("Load after Save: %v", err)
	}
}

func TestLoadReportsAMissingFileAsNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	_, err := Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load of a missing file: err = %v, want one matching fs.ErrNotExist", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"store": "/papers", "stroe": "/typo"}`), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted an unknown field, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should name the file %q", err, path)
	}
}
