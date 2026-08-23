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
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/store"
)

func init() {
	commands = append(commands, command{"init", "create a paper store and point the config at it", runInit})
}

// runInit prepares a directory as a paper store and records it in the
// config file, which is what every other command reads to find the store.
//
// Nothing is created until the config has been checked: a run that would
// silently move an existing setup to a different store must leave both
// the old store and the new directory as they were.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	email := fs.String("email", "", "contact address to send to Crossref and Unpaywall")
	force := fs.Bool("force", false, "point the config at a different store than before")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("init: parsing arguments: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("init: usage: paper init [-email <address>] [-force] <dir>")
	}

	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	cfgPath, err := config.Path()
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		cfg = &config.Config{}
	case err != nil:
		return fmt.Errorf("init: %w", err)
	}
	if cfg.Store != "" && cfg.Store != root && !*force {
		return fmt.Errorf("init: %s already points at %s; pass -force to use %s instead",
			cfgPath, cfg.Store, root)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	switch err := store.CheckMarker(root); {
	case errors.Is(err, os.ErrNotExist):
		if err := store.WriteMarker(root); err != nil {
			return fmt.Errorf("init: %w", err)
		}
		fmt.Printf("initialised paper store %s (format %d)\n", root, store.FormatVersion)
	case err != nil:
		return fmt.Errorf("init: %w", err)
	default:
		fmt.Printf("%s is already a paper store (format %d)\n", root, store.FormatVersion)
	}

	cfg.Store = root
	if *email != "" {
		cfg.Email = *email
	}
	if err := cfg.Save(cfgPath); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	fmt.Printf("wrote %s\n", cfgPath)

	if cfg.Email == "" {
		fmt.Println("note: no contact email is configured, so Unpaywall lookups will fail;")
		fmt.Println("      re-run with -email <address> to set one")
	}

	return nil
}
