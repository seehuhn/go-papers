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
	"io"
	"os"
	"path/filepath"
	"testing"

	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/store"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything fn printed. Any error fn wants to report should be checked
// by fn itself (e.g. via t.Fatal), since fn returns nothing.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(data)
}

// initStore creates a fresh directory prepared as a paper store, and
// points $PAPER_CONFIG at a config file naming it, so that a test drives
// the commands exactly as a configured user would. The user's real store
// and real config are never involved.
func initStore(t *testing.T, email string) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.WriteMarker(dir); err != nil {
		t.Fatalf("preparing the store: %v", err)
	}
	writeConfig(t, &config.Config{Store: dir, Email: email})
	return dir
}

// writeConfig writes cfg to a temporary file and points $PAPER_CONFIG at
// it, returning the path.
func writeConfig(t *testing.T, cfg *config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "paper.json")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	t.Setenv(config.EnvVar, path)
	return path
}

// noConfig points $PAPER_CONFIG at a file that does not exist, so that a
// test can see what an unconfigured paper does.
func noConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "paper.json")
	t.Setenv(config.EnvVar, path)
	return path
}

// openConfiguredStore opens the store the current test is configured to
// use, resolving it exactly as a command does. Tests that inspect the
// store after running a command should reach it this way, so that a break
// in the resolution path shows up as a test failure rather than as a
// silently different store.
func openConfiguredStore(t *testing.T) *store.Store {
	t.Helper()
	s, _, err := openStore("")
	if err != nil {
		t.Fatalf("opening the configured store: %v", err)
	}
	return s
}

// captureStderr is captureStdout for os.Stderr: it runs fn with
// os.Stderr redirected to a pipe and returns everything fn printed
// there.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(data)
}
