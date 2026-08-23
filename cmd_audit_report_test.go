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
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestRenderProseGroupsByVerdict(t *testing.T) {
	r := &auditReport{Entries: []auditEntry{
		{Key: "hoef", Existence: "confirmed", StoreKey: "hoeffding_1963", Holdings: "published"},
		{Key: "ghost", Existence: "notFound"},
		{Key: "maybe", Existence: "unverified", Candidates: []auditCandidate{
			{Score: 0.86, Title: "Adaptive methods", Authors: "Jones", Year: 2011, Source: "crossref"}}},
	}}

	out := renderProse(r)

	for _, want := range []string{"hoeffding_1963", "ghost", "0.86", "Adaptive methods", "crossref"} {
		if !strings.Contains(out, want) {
			t.Errorf("prose should mention %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "likely hallucinated") {
		t.Errorf("a notFound entry must be called out as likely hallucinated:\n%s", out)
	}
}

// TestRenderProseConfirmedWithoutStoreMatchHasNoDanglingArrow pins Finding
// 7: an entry confirmed online (no store match at all) must render as a
// bare key, not "key -> " with nothing after the arrow.
func TestRenderProseConfirmedWithoutStoreMatchHasNoDanglingArrow(t *testing.T) {
	r := &auditReport{Entries: []auditEntry{
		{Key: "onlineonly", Existence: "confirmed"},
	}}

	out := renderProse(r)

	if strings.Contains(out, "onlineonly -> ") || strings.Contains(out, "onlineonly ->\n") {
		t.Errorf("confirmed entry without a store match must not have a dangling arrow:\n%s", out)
	}
	if !strings.Contains(out, "Confirmed: onlineonly\n") {
		t.Errorf("confirmed entry without a store match should render as a bare key:\n%s", out)
	}
}

// TestRenderProseNotFoundWithStoreKeyNamesItInstead is defense in depth
// for Finding 1: even if a notFound verdict somehow carries a StoreKey
// (run() should have clamped it to unverified before this point), the
// prose must never accuse the entry of being likely hallucinated without
// printing the store key as counter-evidence.
func TestRenderProseNotFoundWithStoreKeyNamesItInstead(t *testing.T) {
	r := &auditReport{Entries: []auditEntry{
		{Key: "ghost", Existence: "notFound", StoreKey: "ghost_2020"},
	}}

	out := renderProse(r)

	if strings.Contains(out, "ghost: no source returned any candidate — likely hallucinated") {
		t.Errorf("a store-held key must never be accused without its counter-evidence:\n%s", out)
	}
	if !strings.Contains(out, "ghost_2020") {
		t.Errorf("output should name the store key:\n%s", out)
	}
}

func TestRenderJSONIsParseable(t *testing.T) {
	r := &auditReport{Entries: []auditEntry{{Key: "k", Existence: "confirmed", StoreKey: "k_1999"}}}

	out, err := renderJSON(r)
	if err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var back auditReport
	if err := json.Unmarshal([]byte(out), &back, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if back.Entries[0].StoreKey != "k_1999" {
		t.Errorf("StoreKey = %q", back.Entries[0].StoreKey)
	}
}
