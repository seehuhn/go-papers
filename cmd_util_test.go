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
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/store"
)

func TestOpenStoreUsesTheStoreNamedInTheConfig(t *testing.T) {
	dir := initStore(t, "voss@example.org")

	s, cfg, err := openStore("")
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if s.Root != dir {
		t.Errorf("Root = %q, want %q", s.Root, dir)
	}
	if cfg.Email != "voss@example.org" {
		t.Errorf("Email = %q, want %q", cfg.Email, "voss@example.org")
	}
}

func TestOpenStoreFlagOverridesTheConfig(t *testing.T) {
	initStore(t, "voss@example.org")
	other := t.TempDir()
	if err := store.WriteMarker(other); err != nil {
		t.Fatalf("preparing the store: %v", err)
	}

	s, cfg, err := openStore(other)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if s.Root != other {
		t.Errorf("Root = %q, want %q", s.Root, other)
	}
	if cfg.Email != "voss@example.org" {
		t.Errorf("the flag should move the store, not drop the config: Email = %q", cfg.Email)
	}
}

func TestOpenStoreWorksFromTheFlagWithoutAConfigFile(t *testing.T) {
	noConfig(t)
	dir := t.TempDir()
	if err := store.WriteMarker(dir); err != nil {
		t.Fatalf("preparing the store: %v", err)
	}

	s, _, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if s.Root != dir {
		t.Errorf("Root = %q, want %q", s.Root, dir)
	}
}

func TestOpenStoreWithoutAConfigFilePointsAtInit(t *testing.T) {
	path := noConfig(t)

	_, _, err := openStore("")

	if err == nil {
		t.Fatal("openStore succeeded without a config, want an error")
	}
	if !strings.Contains(err.Error(), "paper init") {
		t.Errorf("error %q should point at `paper init`", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should name the config file it looked for, %q", err, path)
	}
}

func TestOpenStoreWithAConfigThatNamesNoStorePointsAtInit(t *testing.T) {
	writeConfig(t, &config.Config{Email: "voss@example.org"})

	_, _, err := openStore("")

	if err == nil {
		t.Fatal("openStore succeeded with a store-less config, want an error")
	}
	if !strings.Contains(err.Error(), "paper init") {
		t.Errorf("error %q should point at `paper init`", err)
	}
}

func TestOpenStoreReportsAnUninitialisedStoreDirectory(t *testing.T) {
	plain := t.TempDir()
	writeConfig(t, &config.Config{Store: plain})

	_, _, err := openStore("")

	if err == nil {
		t.Fatal("openStore accepted a directory without a marker, want an error")
	}
	if !strings.Contains(err.Error(), "paper init") {
		t.Errorf("error %q should point at `paper init`", err)
	}
	_ = filepath.Join
}
