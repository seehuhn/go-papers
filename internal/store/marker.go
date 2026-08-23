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
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
)

// FormatVersion is the layout version this paper reads and writes. It is
// recorded in every store's marker file, so that a store written by a
// later paper is refused rather than misread.
const FormatVersion = 1

// markerFile is the name of the file in the store root that identifies the
// directory as a paper store. Keys skips it along with every other
// non-directory entry.
const markerFile = ".paper-store.json"

// marker is the content of the marker file. Its single field carries both
// meanings: that this is a paper store, and which format it is in.
type marker struct {
	PaperStore int `json:"paperStore"`
}

// WriteMarker records the current format version in root, making it a
// paper store. An existing marker is overwritten.
func WriteMarker(root string) error {
	data, err := json.Marshal(&marker{PaperStore: FormatVersion}, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("writing the store marker: encoding JSON: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(root, markerFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing the store marker: %w", err)
	}
	return nil
}

// CheckMarker verifies that root carries a marker whose format version
// this paper understands. A missing marker is reported as an error
// matching fs.ErrNotExist, which is how callers tell "not a store" from
// "a store I cannot read".
//
// Unknown members are tolerated on purpose: a later format may add
// fields, and the version mismatch is the message worth showing then, not
// a parse error naming one field of many.
func CheckMarker(root string) error {
	path := filepath.Join(root, markerFile)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the store marker: %w", err)
	}

	var m marker
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("reading the store marker %s: %w", path, err)
	}

	if m.PaperStore != FormatVersion {
		return fmt.Errorf(
			"%s is in store format %d, but this paper understands format %d; upgrade paper",
			root, m.PaperStore, FormatVersion)
	}
	return nil
}
