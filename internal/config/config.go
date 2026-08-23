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

// Package config reads and writes the per-user configuration file, which
// says where the paper store is and which contact address to give the
// online services. It sits above the store rather than inside it: a store
// cannot be opened until the config has said where to look.
package config

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
)

// EnvVar names the environment variable that overrides the location of the
// config file. It holds the path of the file itself, not of a directory.
const EnvVar = "PAPER_CONFIG"

// defaultName is the config file's name within the user's home directory.
const defaultName = ".paper.json"

// Config holds the settings shared by all paper commands.
type Config struct {
	// Store is the absolute path of the paper store's root directory.
	Store string `json:"store"`

	// Email is the contact address sent to Crossref and Unpaywall. It is
	// optional, but Unpaywall refuses to answer without it.
	Email string `json:"email,omitzero"`
}

// Path returns the location of the config file: the value of $PAPER_CONFIG
// when that is set, and ~/.paper.json otherwise.
func Path() (string, error) {
	if p := os.Getenv(EnvVar); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating the config file: %w", err)
	}
	return filepath.Join(home, defaultName), nil
}

// Load reads and parses the config file at path. A missing file is
// reported as an error matching fs.ErrNotExist, since without it nothing
// knows where the store is.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}

	var c Config
	if err := json.Unmarshal(data, &c, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}

	return &c, nil
}

// Save writes c to path, creating the parent directory if needed. The
// write is atomic and the file is readable only by its owner, since it
// holds an email address.
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("saving config: creating directory %s: %w", dir, err)
	}

	data, err := json.Marshal(c, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("saving config: encoding JSON: %w", err)
	}
	data = append(data, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("saving config: writing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("saving config: renaming %s to %s: %w", tmpPath, path, err)
	}

	return nil
}
