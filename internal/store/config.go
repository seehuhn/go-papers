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
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
)

// configFile is the name of the store-wide configuration file, held in the
// store root.
const configFile = "config.json"

// Config holds store-wide settings, read from config.json in the store
// root.
type Config struct {
	Email string `json:"email,omitzero"`
}

// LoadConfig reads and parses the store's config.json file. A missing file
// is not an error: it returns an empty Config.
func (s *Store) LoadConfig() (*Config, error) {
	path := filepath.Join(s.Root, configFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}

	var c Config
	if err := json.Unmarshal(data, &c, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("loading config %s: %w", path, err)
	}

	return &c, nil
}
