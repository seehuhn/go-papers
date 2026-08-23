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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMarkerRecordsTheCurrentFormatVersion(t *testing.T) {
	dir := t.TempDir()

	if err := WriteMarker(dir); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".paper-store.json"))
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	want := "{\n  \"paperStore\": 1\n}\n"
	if string(data) != want {
		t.Errorf("marker = %q, want %q", data, want)
	}
}

func TestCheckMarkerAcceptsAWrittenMarker(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMarker(dir); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	if err := CheckMarker(dir); err != nil {
		t.Errorf("CheckMarker: %v", err)
	}
}

func TestCheckMarkerReportsAMissingMarkerAsNotExist(t *testing.T) {
	err := CheckMarker(t.TempDir())

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("CheckMarker on a plain directory: err = %v, want one matching fs.ErrNotExist", err)
	}
}

func TestCheckMarkerRejectsANewerFormatVersion(t *testing.T) {
	dir := t.TempDir()
	writeMarkerVersion(t, dir, 2)

	err := CheckMarker(dir)

	if err == nil {
		t.Fatal("CheckMarker accepted format version 2, want an error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a marker with an unknown version must not read as missing: %v", err)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error %q should name the version it found", err)
	}
}

func TestOpenAcceptsAnInitialisedStore(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMarker(dir); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Root != dir {
		t.Errorf("Root = %q, want %q", s.Root, dir)
	}
}

func TestOpenRejectsADirectoryThatIsNotAStore(t *testing.T) {
	dir := t.TempDir()

	_, err := Open(dir)

	if err == nil {
		t.Fatal("Open accepted a directory without a marker, want an error")
	}
	if !strings.Contains(err.Error(), "paper init") {
		t.Errorf("error %q should point at `paper init`", err)
	}
}

func TestOpenRejectsAMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere")

	if _, err := Open(missing); err == nil {
		t.Error("Open accepted a missing directory, want an error")
	}
}

// writeMarkerVersion writes a store marker claiming the given format
// version, so that a test can present a store written by another paper.
func writeMarkerVersion(t *testing.T, dir string, version int) {
	t.Helper()
	data := []byte(strings.Replace("{\n  \"paperStore\": V\n}\n", "V", string(rune('0'+version)), 1))
	if err := os.WriteFile(filepath.Join(dir, ".paper-store.json"), data, 0o644); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}
}
