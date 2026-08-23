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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/store"
)

func TestInitCreatesTheStoreAndTheConfig(t *testing.T) {
	cfgPath := noConfig(t)
	root := filepath.Join(t.TempDir(), "Papers")

	out := captureStdout(t, func() {
		if err := runInit([]string{"-email", "voss@example.org", root}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})

	if err := store.CheckMarker(root); err != nil {
		t.Errorf("the new directory is not a paper store: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading the config init wrote: %v", err)
	}
	if cfg.Store != root {
		t.Errorf("Store = %q, want %q", cfg.Store, root)
	}
	if cfg.Email != "voss@example.org" {
		t.Errorf("Email = %q, want %q", cfg.Email, "voss@example.org")
	}
	for _, want := range []string{root, cfgPath} {
		if !strings.Contains(out, want) {
			t.Errorf("output should name %q:\n%s", want, out)
		}
	}
}

func TestInitRecordsAnAbsolutePath(t *testing.T) {
	cfgPath := noConfig(t)
	t.Chdir(t.TempDir())

	if err := runInit([]string{"Papers"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	if !filepath.IsAbs(cfg.Store) {
		t.Errorf("Store = %q, want an absolute path", cfg.Store)
	}
}

func TestInitOnAnAlreadyInitialisedStoreAdoptsIt(t *testing.T) {
	cfgPath := noConfig(t)
	root := t.TempDir()
	if err := store.WriteMarker(root); err != nil {
		t.Fatalf("preparing the store: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runInit([]string{root}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})

	if !strings.Contains(out, "already") {
		t.Errorf("output should say the store already exists:\n%s", out)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	if cfg.Store != root {
		t.Errorf("Store = %q, want %q", cfg.Store, root)
	}
}

func TestInitRefusesToRepointTheConfig(t *testing.T) {
	first := initStore(t, "voss@example.org")
	cfgPath := os.Getenv(config.EnvVar)
	second := filepath.Join(t.TempDir(), "Other")

	err := runInit([]string{second})

	if err == nil {
		t.Fatal("init repointed the config without -force, want an error")
	}
	if !strings.Contains(err.Error(), first) {
		t.Errorf("error %q should name the store the config points at", err)
	}
	if !strings.Contains(err.Error(), "-force") {
		t.Errorf("error %q should mention -force", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	if cfg.Store != first {
		t.Errorf("the config changed anyway: Store = %q, want %q", cfg.Store, first)
	}
	if _, err := os.Stat(second); err == nil {
		t.Error("the refused store directory should not have been created")
	}
}

func TestInitForceRepointsTheConfig(t *testing.T) {
	initStore(t, "voss@example.org")
	cfgPath := os.Getenv(config.EnvVar)
	second := filepath.Join(t.TempDir(), "Other")

	captureStdout(t, func() {
		if err := runInit([]string{"-force", second}); err != nil {
			t.Fatalf("init -force: %v", err)
		}
	})

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	if cfg.Store != second {
		t.Errorf("Store = %q, want %q", cfg.Store, second)
	}
	if cfg.Email != "voss@example.org" {
		t.Errorf("-force moved the store but should have kept the email: %q", cfg.Email)
	}
}

func TestInitRejectsAStoreInAnUnknownFormat(t *testing.T) {
	cfgPath := noConfig(t)
	root := t.TempDir()
	marker := filepath.Join(root, ".paper-store.json")
	if err := os.WriteFile(marker, []byte("{\"paperStore\": 2}\n"), 0o644); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	err := runInit([]string{root})

	if err == nil {
		t.Fatal("init accepted a store in format 2, want an error")
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("no config should have been written for a store paper cannot read")
	}
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), "2") {
		t.Errorf("the existing marker must be left alone, got %q (%v)", data, err)
	}
}

func TestInitWithoutAnEmailSaysSo(t *testing.T) {
	noConfig(t)
	root := filepath.Join(t.TempDir(), "Papers")

	out := captureStdout(t, func() {
		if err := runInit([]string{root}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})

	if !strings.Contains(out, "-email") {
		t.Errorf("output should say how to set the missing contact email:\n%s", out)
	}
}

func TestInitNeedsExactlyOneDirectory(t *testing.T) {
	noConfig(t)

	if err := runInit(nil); err == nil {
		t.Error("init with no directory should fail")
	}
	if err := runInit([]string{"a", "b"}); err == nil {
		t.Error("init with two directories should fail")
	}
}
