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

package resolve

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchFilePDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4\n%fake pdf content")
	}))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "sub", "out.pdf")
	if err := FetchFile(srv.Client(), srv.URL, dest, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !strings.HasPrefix(string(data), "%PDF-") {
		t.Errorf("stored file bad: %q, %v", data, err)
	}
}

func TestFetchFileRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html>Please sign in</html>")
	}))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "out.pdf")
	err := FetchFile(srv.Client(), srv.URL, dest, "")
	if err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Errorf("got %v, want a 'not a PDF' error", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("no file must be stored on rejection")
	}
}

func gzipTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		if err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(content))
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func TestFetchSourceTarball(t *testing.T) {
	body := gzipTarball(t, map[string]string{"main.tex": `\documentclass{article}`, "refs.bib": "@misc{x,}"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "arxiv-2412.05039v2")
	if err := FetchSource(srv.Client(), srv.URL, dir, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.tex"))
	if err != nil || !strings.Contains(string(data), "documentclass") {
		t.Errorf("main.tex: %q, %v", data, err)
	}
}

func TestFetchSourceSingleFile(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	io.WriteString(gw, `\documentclass{article} single-file submission`)
	gw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "src")
	if err := FetchSource(srv.Client(), srv.URL, dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "main.tex")); err != nil {
		t.Error("single-file source should land as main.tex")
	}
}

func TestFetchSourcePDFOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "%PDF-1.4 pdf-only submission")
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "src")
	err := FetchSource(srv.Client(), srv.URL, dir, "")
	if !errors.Is(err, ErrPDFOnly) {
		t.Errorf("got %v, want ErrPDFOnly", err)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("destDir must be removed for PDF-only submissions")
	}
}

func TestFetchSourceRejectsTraversal(t *testing.T) {
	body := gzipTarball(t, map[string]string{"../evil.tex": "x"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	parent := t.TempDir()
	dir := filepath.Join(parent, "src")
	err := FetchSource(srv.Client(), srv.URL, dir, "")
	if err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(parent, "evil.tex")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("file escaped the destination directory")
	}
}

func TestFetchFileRejectsOversizedBody(t *testing.T) {
	saved := maxDownloadSize
	maxDownloadSize = 1024
	t.Cleanup(func() { maxDownloadSize = saved })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("%PDF-1.4\n"))
		w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "out.pdf")
	err := FetchFile(srv.Client(), srv.URL, dest, "")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversized body must error mentioning the size cap, got %v", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("no file may be stored when the cap is exceeded")
	}
}

func TestFetchSourceRejectsOversizedBody(t *testing.T) {
	saved := maxDownloadSize
	maxDownloadSize = 1024
	t.Cleanup(func() { maxDownloadSize = saved })

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(bytes.Repeat([]byte("a"), 4096)) // decompresses over the cap
	gw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "src")
	err := FetchSource(srv.Client(), srv.URL, dir, "")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversized decompressed source must error, got %v", err)
	}
	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("destDir must not remain when the cap is exceeded")
	}
}
