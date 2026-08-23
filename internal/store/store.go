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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// paperFile is the name of the file holding a paper's state within its
// directory in the store.
const paperFile = "paper.json"

// Store is a directory containing one subdirectory per paper.
type Store struct {
	Root string
}

// Open returns a Store backed by the directory at root, which must exist
// and carry a store marker in a format this paper understands. Resolving
// root — from the -store flag or from the config file — happens in the
// caller; this package has no opinion on where a store lives.
func Open(root string) (*Store, error) {
	info, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("paper store directory %q does not exist", root)
	case err != nil:
		// A permissions problem is not absence; keep the cause visible.
		return nil, fmt.Errorf("paper store directory %q: %w", root, err)
	case !info.IsDir():
		return nil, fmt.Errorf("paper store %q is not a directory", root)
	}

	if err := CheckMarker(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf(
				"%s is not a paper store: run `paper init %s` to create one", root, root)
		}
		return nil, err
	}

	return &Store{Root: root}, nil
}

// Dir returns the path of the directory holding the paper with the given
// key.
func (s *Store) Dir(key string) string {
	return filepath.Join(s.Root, key)
}

// Load reads and parses the paper.json file for the paper with the given
// key.
func (s *Store) Load(key string) (*Paper, error) {
	path := filepath.Join(s.Dir(key), paperFile)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading paper %q: %w", key, err)
	}

	var p Paper
	if err := json.Unmarshal(data, &p, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("loading paper %q from %s: %w", key, path, err)
	}

	return &p, nil
}

// Save writes p to its paper.json file, creating the paper's directory if
// necessary. The write is atomic: the new content is written to a
// temporary file in the same directory and then renamed into place.
func (s *Store) Save(p *Paper) error {
	dir := s.Dir(p.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("saving paper %q: creating directory %s: %w", p.Key, dir, err)
	}

	data, err := json.Marshal(p, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("saving paper %q: encoding JSON: %w", p.Key, err)
	}
	data = append(data, '\n')

	finalPath := filepath.Join(dir, paperFile)
	tmpPath := finalPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("saving paper %q: writing %s: %w", p.Key, tmpPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("saving paper %q: renaming %s to %s: %w", p.Key, tmpPath, finalPath, err)
	}

	return nil
}

// Keys returns the sorted keys of all papers in the store, i.e. the names
// of the subdirectories of the store root that contain a paper.json file.
// Other entries in the store root (files, or directories without a
// paper.json) are silently skipped.
func (s *Store) Keys() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, fmt.Errorf("listing paper store %s: %w", s.Root, err)
	}

	var keys []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.Root, e.Name(), paperFile)); err != nil {
			continue
		}
		keys = append(keys, e.Name())
	}
	sort.Strings(keys)

	return keys, nil
}

// LoadAll loads every paper in the store, in key order.
func (s *Store) LoadAll() ([]*Paper, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, err
	}

	papers := make([]*Paper, 0, len(keys))
	for _, key := range keys {
		p, err := s.Load(key)
		if err != nil {
			return nil, err
		}
		papers = append(papers, p)
	}

	return papers, nil
}
