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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Attach moves the file at srcPath into the directory of paper p, under
// the given filename, and records the acquisition in p's metadata. It
// refuses to overwrite an existing file at the destination. On any
// failure, srcPath is left in place.
//
// The move is done with os.Rename where possible, falling back to a
// copy-then-remove for moves across filesystem boundaries.
func (s *Store) Attach(p *Paper, srcPath, filename, source string, now time.Time) error {
	dstPath := filepath.Join(s.Dir(p.Key), filename)

	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("attaching %s: %s already exists", filename, dstPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("attaching %s: checking %s: %w", filename, dstPath, err)
	}

	if err := os.MkdirAll(s.Dir(p.Key), 0o755); err != nil {
		return fmt.Errorf("attaching %s: creating directory %s: %w", filename, s.Dir(p.Key), err)
	}

	if err := moveFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("attaching %s: %w", filename, err)
	}

	if p.Versions == nil {
		p.Versions = make(map[string]Version)
	}
	p.Versions[filename] = Version{
		Acquired: now.Format("2006-01-02"),
		Source:   source,
	}
	RecomputeHoldings(p)
	p.AppendLog(now, "attach", filename+" from "+source)

	if err := s.Save(p); err != nil {
		return fmt.Errorf("attaching %s: %w", filename, err)
	}

	return nil
}

// moveFile moves the file at src to dst. It tries os.Rename first, and
// falls back to a copy followed by removing the source when the rename
// fails because src and dst are on different filesystems.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("moving %s to %s: %w", src, dst, err)
	}

	if copyErr := copyFile(src, dst); copyErr != nil {
		return fmt.Errorf("moving %s to %s: cross-device rename, copy fallback failed: %w", src, dst, copyErr)
	}
	if rmErr := os.Remove(src); rmErr != nil {
		return fmt.Errorf("moving %s to %s: copied but failed to remove source: %w", src, dst, rmErr)
	}
	return nil
}

// copyFile copies the file at src to dst, refusing to overwrite an
// existing file, and cleaning up a partial destination file on failure.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}

	return nil
}

// RecomputeHoldings sets p.Holdings from p.Versions: a version whose name
// starts with "arxiv-" counts as a preprint; any other ".pdf" file counts
// as published. Deprecated versions still count as held.
func RecomputeHoldings(p *Paper) {
	var hasPreprint, hasPublished bool
	for name := range p.Versions {
		switch {
		case strings.HasPrefix(name, "arxiv-"):
			hasPreprint = true
		case strings.HasSuffix(name, ".pdf"):
			hasPublished = true
		}
	}

	switch {
	case hasPreprint && hasPublished:
		p.Holdings = "both"
	case hasPreprint:
		p.Holdings = "preprint"
	case hasPublished:
		p.Holdings = "published"
	default:
		p.Holdings = "none"
	}
}

// AppendLog appends a log entry to p.Log, recording now, action, and
// detail.
func (p *Paper) AppendLog(now time.Time, action, detail string) {
	p.Log = append(p.Log, LogEntry{
		When:   now.Format("2006-01-02T15:04:05"),
		Action: action,
		Detail: detail,
	})
}
