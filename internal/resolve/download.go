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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"seehuhn.de/go/paper/internal/sources"
)

// maxDownloadSize caps the number of bytes read from an HTTP response
// body, to protect against unexpectedly large or malicious responses.
// arXiv e-prints and published PDFs are ordinarily well under this, but
// a small handful of PDFs (e.g. with embedded supplementary data) can
// run to tens of MB, so the ceiling is generous.
var maxDownloadSize int64 = 100 << 20 // 100 MB

// pdfMagic is the byte sequence that starts every valid PDF file.
const pdfMagic = "%PDF-"

// gzipMagic is the two-byte magic number that starts every gzip stream.
var gzipMagic = []byte{0x1f, 0x8b}

// ErrPDFOnly is returned by FetchSource when the arXiv submission has no
// tex source, only a PDF.
var ErrPDFOnly = errors.New("arXiv submission is PDF-only, no tex source")

// newDownloadRequest builds a GET request for url carrying the standard
// polite-pool User-Agent header.
func newDownloadRequest(url, email string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sources.UserAgent(email))
	return req, nil
}

// FetchFile downloads url to destPath (creating parent directories),
// verifying the content starts with %PDF-. Non-PDF content (HTML
// interstitials) is discarded and reported as an error naming the URL
// and the content-type received. The download goes to destPath+".tmp"
// first and is renamed only on success.
func FetchFile(client *http.Client, url, destPath, email string) error {
	req, err := newDownloadRequest(url, email)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s for %s", resp.Status, url)
	}

	limited := io.LimitReader(resp.Body, maxDownloadSize+1)
	var header [len(pdfMagic)]byte
	n, err := io.ReadFull(limited, header[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("reading %s: %w", url, err)
	}
	if string(header[:n]) != pdfMagic {
		ct := resp.Header.Get("Content-Type")
		return fmt.Errorf("%s is not a PDF (content-type %q)", url, ct)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	written := int64(n)
	_, werr := f.Write(header[:n])
	if werr == nil {
		var copied int64
		copied, werr = io.Copy(f, limited)
		written += copied
	}
	cerr := f.Close()
	if written > maxDownloadSize {
		os.Remove(tmpPath)
		return fmt.Errorf("response from %s is too large (over %d bytes)", url, maxDownloadSize)
	}
	if werr != nil || cerr != nil {
		os.Remove(tmpPath)
		if werr != nil {
			return fmt.Errorf("writing %s: %w", tmpPath, werr)
		}
		return fmt.Errorf("closing %s: %w", tmpPath, cerr)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// FetchSource downloads an arXiv e-print (gzip, possibly a tarball) and
// extracts it into destDir. A gzipped tarball extracts fully (paths
// sanitized against traversal); a gzipped single file becomes
// destDir/main.tex; a raw %PDF- response means the submission is
// PDF-only: FetchSource removes destDir and returns ErrPDFOnly.
func FetchSource(client *http.Client, url, destDir, email string) error {
	req, err := newDownloadRequest(url, email)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s for %s", resp.Status, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return fmt.Errorf("reading %s: %w", url, err)
	}
	if int64(len(body)) > maxDownloadSize {
		return fmt.Errorf("response from %s is too large (over %d bytes)", url, maxDownloadSize)
	}

	if strings.HasPrefix(string(body), pdfMagic) {
		if err := os.RemoveAll(destDir); err != nil {
			return err
		}
		return ErrPDFOnly
	}

	if len(body) < 2 || !bytes.Equal(body[:2], gzipMagic) {
		return fmt.Errorf("%s is not a gzip stream or PDF", url)
	}

	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("decompressing %s: %w", url, err)
	}
	decompressed, err := io.ReadAll(io.LimitReader(gz, maxDownloadSize+1))
	if err != nil {
		return fmt.Errorf("decompressing %s: %w", url, err)
	}
	if int64(len(decompressed)) > maxDownloadSize {
		os.RemoveAll(destDir)
		return fmt.Errorf("response from %s is too large (over %d bytes)", url, maxDownloadSize)
	}

	tr := tar.NewReader(bytes.NewReader(decompressed))
	_, tarErr := tr.Next()
	if tarErr != nil {
		// Not a tarball (or an empty/degenerate one): treat the
		// decompressed content as a single tex file.
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		mainPath := filepath.Join(destDir, "main.tex")
		if err := os.WriteFile(mainPath, decompressed, 0o644); err != nil {
			os.RemoveAll(destDir)
			return err
		}
		return nil
	}

	if err := extractTar(bytes.NewReader(decompressed), destDir); err != nil {
		os.RemoveAll(destDir)
		return err
	}
	return nil
}

// extractTar extracts the tar stream r into destDir, creating destDir
// and any needed subdirectories. Entries whose path would escape
// destDir are rejected; non-regular entries (symlinks, devices, etc.)
// are skipped.
func extractTar(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		target := filepath.Join(destDir, hdr.Name)
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(absTarget, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(absTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			cerr := f.Close()
			if err != nil {
				return fmt.Errorf("writing %s: %w", absTarget, err)
			}
			if cerr != nil {
				return fmt.Errorf("closing %s: %w", absTarget, cerr)
			}
		default:
			// Skip symlinks, devices, and other non-regular entries.
		}
	}
}
