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
	"strings"
	"testing"

	"seehuhn.de/go/paper/internal/bibtex"
	"seehuhn.de/go/paper/internal/store"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything fn printed, alongside fn's own return value.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(data), runErr
}

func fixtureStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PAPER_STORE", dir)
	return &store.Store{Root: dir}, dir
}

func cleanPaper(key string) *store.Paper {
	return &store.Paper{Key: key, Status: "clean", Holdings: "none",
		DOI: "10.1080/01621459.1963.10500830",
		Bibtex: bibtex.Entry{Type: "article", Fields: map[string]string{
			"author":  "Hoeffding, Wassily",
			"title":   "Probability inequalities for sums of bounded random variables",
			"journal": "Journal of the American Statistical Association",
			"year":    "1963",
			"doi":     "10.1080/01621459.1963.10500830",
		}}}
}

func TestCheckCleanStore(t *testing.T) {
	s, _ := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	if err := runCheck(nil); err != nil {
		t.Errorf("clean store: %v", err)
	}
}

func TestCheckFindsKeyMismatch(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	s.Save(p)
	// dirname/key mismatch: rename the directory
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))
	if err := runCheck(nil); err == nil {
		t.Error("expected error for key/dirname mismatch")
	}
}

func TestCheckFindsSyncConflict(t *testing.T) {
	s, dir := fixtureStore(t)
	s.Save(cleanPaper("hoeffding_1963"))
	os.WriteFile(filepath.Join(dir, "hoeffding_1963",
		"paper.sync-conflict-20260820-XYZ.json"), []byte("{}"), 0o644)
	if err := runCheck(nil); err == nil {
		t.Error("expected error for sync-conflict file")
	}
}

func TestCheckPromotesDraft(t *testing.T) {
	s, _ := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.Status = "draft"
	s.Save(p)
	if err := runCheck(nil); err != nil {
		t.Fatalf("check: %v", err)
	}
	got, _ := s.Load("hoeffding_1963")
	if got.Status != "clean" {
		t.Errorf("status = %q, want clean", got.Status)
	}
}

func TestCheckUnknownKeyArg(t *testing.T) {
	fixtureStore(t)
	if err := runCheck([]string{"nope_1900"}); err == nil {
		t.Error("expected error for unknown key")
	}
}

// TestCheckMismatchReportsUnderDirName covers an entry that is both
// dirname/key mismatched AND has its own CheckPaper error (an invalid
// DOI). Both problems must be printed under the directory's name
// ("wrong_1963"), not under the stale key recorded inside paper.json
// ("hoeffding_1963") - the directory name is the only identity that
// still resolves to a loadable entry.
func TestCheckMismatchReportsUnderDirName(t *testing.T) {
	s, dir := fixtureStore(t)
	p := cleanPaper("hoeffding_1963")
	p.DOI = "not-a-doi"
	p.Bibtex.Fields["doi"] = "not-a-doi"
	s.Save(p)
	os.Rename(filepath.Join(dir, "hoeffding_1963"), filepath.Join(dir, "wrong_1963"))

	out, err := captureStdout(t, func() error { return runCheck(nil) })
	if err == nil {
		t.Fatal("expected error for key/dirname mismatch plus DOI problem")
	}

	if strings.Contains(out, "hoeffding_1963:") {
		t.Errorf("output still uses the stale paper.json key, want only the directory name:\n%s", out)
	}
	if !strings.Contains(out, `wrong_1963: error: directory name "wrong_1963" does not match paper.json key "hoeffding_1963"`) {
		t.Errorf("missing dirname/key mismatch line under the directory name:\n%s", out)
	}
	if !strings.Contains(out, "wrong_1963: error: doi:") {
		t.Errorf("missing CheckPaper DOI error line under the directory name:\n%s", out)
	}
}

// TestCheckCorruptEntryReportedOnce covers a corrupt paper.json during a
// whole-store run: runCheck loads every entry once, and both the
// per-entry CheckPaper pass and fsck's store-wide pass need that same
// load result, so the "cannot load entry" problem must appear exactly
// once, not once per pass.
func TestCheckCorruptEntryReportedOnce(t *testing.T) {
	_, dir := fixtureStore(t)
	entryDir := filepath.Join(dir, "broken_1900")
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatalf("creating entry directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "paper.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("writing corrupt paper.json: %v", err)
	}

	out, err := captureStdout(t, func() error { return runCheck(nil) })
	if err == nil {
		t.Fatal("expected error for corrupt paper.json")
	}

	count := strings.Count(out, "cannot load entry")
	if count != 1 {
		t.Errorf(`"cannot load entry" appeared %d times, want exactly 1:%s%s`, count, "\n", out)
	}
}
