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
	"sort"
)

// paperFile is the name of the file holding a paper's state within its
// directory in the store.
const paperFile = "paper.json"

// Store is a directory containing one subdirectory per paper.
type Store struct {
	Root string
}

// Open resolves the store root and returns a Store backed by it. The root
// is chosen from, in order of preference: flagValue (if non-empty), the
// PAPER_STORE environment variable, and $HOME/Papers. Open fails if
// the resolved directory does not exist.
func Open(flagValue string) (*Store, error) {
	envValue := os.Getenv("PAPER_STORE")

	home, homeErr := os.UserHomeDir()
	var defaultDir string
	if homeErr == nil {
		defaultDir = filepath.Join(home, "Papers")
	}

	root := flagValue
	if root == "" {
		root = envValue
	}
	if root == "" {
		root = defaultDir
	}

	if root == "" {
		return nil, fmt.Errorf(
			"cannot resolve paper store: no --store flag given, PAPER_STORE is not set, and $HOME could not be determined (%v)",
			homeErr)
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"paper store directory %q does not exist (tried, in order: --store flag %q, PAPER_STORE=%q, default %q)",
			root, flagValue, envValue, defaultDir)
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
