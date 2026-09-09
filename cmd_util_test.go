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
	"path/filepath"
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/config"
	"seehuhn.de/go/paper/internal/match"
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

func TestEventDetail(t *testing.T) {
	if got := eventDetail(nil); got != "" {
		t.Errorf("nil error: got %q", got)
	}
	if got := eventDetail(errors.New("first line\nsecond line")); got != "first line" {
		t.Errorf("multi-line: got %q", got)
	}
	long := strings.Repeat("x", eventDetailMax+50)
	if got := eventDetail(errors.New(long)); len([]rune(got)) != eventDetailMax || !strings.HasSuffix(got, "…") {
		t.Errorf("long message: got %d runes, suffix %q", len([]rune(got)), got[len(got)-3:])
	}
}

// findByTitleFixture saves one held entry, "A Book About Widgets" by Ann
// Author, that the findByTitle tests match against.
func findByTitleFixture(t *testing.T) *store.Store {
	t.Helper()
	initStore(t, "test@example.org")
	s := openConfiguredStore(t)
	if err := s.Save(&store.Paper{Key: "widgets_1994", Status: "clean", Holdings: "published",
		Bibtex: bibtex.Entry{Type: "book", Fields: map[string]string{
			"author": "Author, Ann", "title": "A Book About Widgets", "year": "1994"}}}); err != nil {
		t.Fatalf("saving the fixture entry: %v", err)
	}
	return s
}

func TestFindByTitleMatchesOnTitleAndSurname(t *testing.T) {
	s := findByTitleFixture(t)

	key, err := findByTitle(s, "A Book About Widgets", "Author")
	if err != nil {
		t.Fatal(err)
	}
	if key != "widgets_1994" {
		t.Errorf("key = %q, want widgets_1994", key)
	}
}

func TestFindByTitleWrongSurnameNoMatch(t *testing.T) {
	s := findByTitleFixture(t)

	key, err := findByTitle(s, "A Book About Widgets", "Somebody")
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Errorf("key = %q, want no match", key)
	}
}

// TestFindByTitleNearMissDoesNotMatch is the regression case titleBar was
// raised for: five shared tokens out of six score 0.833, and the surname
// agrees, so the title bar is the only thing that can refuse the match.
func TestFindByTitleNearMissDoesNotMatch(t *testing.T) {
	initStore(t, "test@example.org")
	s := openConfiguredStore(t)
	if err := s.Save(&store.Paper{Key: "widgets_1994", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "book", Fields: map[string]string{
			"author": "Author, Ann", "title": "A Short Book About Widgets", "year": "1994"}}}); err != nil {
		t.Fatalf("saving the fixture entry: %v", err)
	}
	const near = "A Short Book About Small Widgets"
	if got := match.TitleSimilarity(near, "A Short Book About Widgets"); got < 0.83 || got > 0.84 {
		t.Fatalf("the pair scores %v; the case is meant to be the ~0.833 near miss", got)
	}

	key, err := findByTitle(s, near, "Author")
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Errorf("key = %q, want no match: 0.833 is below the bar", key)
	}
}

func TestFindByTitleSeveralMatchesNoMatch(t *testing.T) {
	s := findByTitleFixture(t)
	if err := s.Save(&store.Paper{Key: "widgets_1996", Status: "clean", Holdings: "none",
		Bibtex: bibtex.Entry{Type: "book", Fields: map[string]string{
			"author": "Author, Ann", "title": "A Book About Widgets", "year": "1996"}}}); err != nil {
		t.Fatal(err)
	}

	key, err := findByTitle(s, "A Book About Widgets", "Author")
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Errorf("key = %q, want no match when several entries fit", key)
	}
}
