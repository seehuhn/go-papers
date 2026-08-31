# paper

`paper` is a CLI tool for managing a local store of scientific papers with bibtex metadata. It provides a companion interface for Claude agents to organize, search, and manage research documents.

To install the latest version, run:

```bash
go install seehuhn.de/go/paper@latest
```

## The store

The store is a plain directory, in a location of your choosing: one
subdirectory per paper, named by its citation key, plus a marker file and
an event log.

```
~/Papers/
├── .paper-store.json      marker: this directory is a paper store
├── events/                local, best-effort log of command outcomes
└── <key>/                 one directory per paper
    ├── paper.json             single source of truth (see schema below)
    ├── published.pdf          version of record, when held
    ├── arxiv-2412.05039v2.pdf arXiv PDF, when held (version-qualified name)
    └── arxiv-2412.05039v2/    extracted arXiv tex source
```

`<key>` is `surname_year` of the first author (lowercase ASCII, disambiguated
`hoeffding_1963a`/`hoeffding_1963b` when needed) and is simultaneously the
directory name and the bibtex key. `paper check` enforces that the two match.

Everything is plain files: no database, no derived index. The store must
stay navigable by a human without tooling.

### `.paper-store.json`

The marker that makes a directory a store, written by `paper init`:

```json
{"paperStore": 1}
```

The number is the layout version. It is checked every time the store is
opened, so a store written by a later `paper` is refused rather than
misread, and a mistyped or not-yet-synced path is reported as such
instead of looking like an empty store.

### `events/`

One JSON line per command outcome, appended to `events/<hostname>.jsonl`:
what was run, what it was given, and how it ended. The log is
best-effort and local — a failure to write it never fails a command, and
each machine writes only its own file, so a synced store never sees a
conflict. No command reads it: it is kept so that what works in practice
can be looked at later.

## Configuration

Settings live in one file per user, outside the store:

```json
{
  "store": "/home/you/Papers",
  "email": "you@example.com"
}
```

`store` is the store root, and is what makes `paper` usable with no
environment set up at all. `email` is the contact address the online
metadata services are given: Unpaywall requires one and refuses to answer
without it, and Crossref uses it to put requests in its faster,
better-behaved "polite pool".

Write the file with `paper init` rather than by hand.

### Where the files are found

The config file is `~/.paper.json`, or the file named by the
`PAPER_CONFIG` environment variable when that is set.

The store root is the `-store` flag when given, and the `store` field of
the config otherwise. `paper` has no built-in default: without a config
file, every command that touches the store fails and says to run `paper
init`.

The usual setup is a single `paper init` naming a directory that is
synced between your machines (e.g. via syncthing).

## Commands

Eight commands are implemented so far.

### `paper help`

Lists the available commands. Also shown when `paper` is run with no
arguments.

```bash
$ paper help
```

### `paper init [-email <address>] [-force] <dir>`

Prepares `<dir>` as a paper store and records it in the config file, so
that every later command finds it without a flag. The directory is
created if it does not exist.

```
$ paper init -email you@example.com ~/Papers
initialised paper store /home/you/Papers (format 1)
wrote /home/you/.paper.json
```

Running it again on the same store is how a second machine adopts an
already-synced store: the marker is left alone and only the config is
written. Pointing the config at a *different* store is refused unless
`-force` is given, so that a mistyped path cannot silently strand the
store you already have. `-email` is optional and, when omitted, leaves
any address already configured in place.

### `paper fetch [-dry-run] [-doi <doi>] [-into <key>] <ref>`

Resolves a reference online and downloads what can be fetched reliably by
script. The reference is a DOI, an arXiv ID or URL, the URL of a PDF, or
free text, which is put through a Crossref search and accepted only when
one hit clearly wins. A DOI gets its metadata from Crossref and its PDF
from Unpaywall, if there is an open-access route; an arXiv ID gets the
preprint's PDF and tex source, with the published record merged in when
the preprint names a DOI. `-dry-run` reports what would happen and writes
nothing.

A PDF URL is the route for a paper or book whose file is openly available
somewhere no script can find it — an author's page, a course site, an
institutional repository. It is the one reference kind that resolves
backwards: a URL names a file and says nothing about the work it holds, so
the file is downloaded first and then identified exactly as `ingest`
identifies a file found on disk. What it was downloaded from is recorded
as the file's source, which is the part a later audit needs and the part
that downloading by hand throws away. Because nothing is known about the
paper until the download succeeds, a failure here creates no entry at all.
`-doi` says what an unstamped file is, skipping identification, and
`-into` attaches it to an entry that already exists.

The failures are hand-offs to the calling agent, not bare errors. Where no
download route exists the draft entry is still created, and the command
exits nonzero reporting what it learned about the paper, what the entry
holds, and where the file should go once found by hand. Free text that
pins down no single paper creates no entry at all, and the error lists the
candidates from Crossref, zbMATH and DBLP to choose between.

```bash
$ paper fetch 10.1080/01621459.1963.10500830
$ paper fetch arXiv:2412.05039
$ paper fetch https://users.aalto.fi/~ssarkka/pub/cup_book_online.pdf
$ paper fetch -into sarkka_2013 https://users.aalto.fi/~ssarkka/pub/cup_book_online.pdf
```

### `paper ingest [-since <ts>] [-into <key>] [-doi <doi>] [-arxiv <id>] <file.pdf>...`

Works out which paper each PDF already on disk is, and moves it into the
store. Identification is tiered: the Info dictionary first, then a DOI or
arXiv stamp in the page text, then the title read off the first page and
searched at Crossref. Nothing is downloaded — the file is already held, so
the services are asked for metadata only. `-since` drops files older than a
timestamp, and `-doi`/`-arxiv` say outright what a single file is, skipping
identification.

Once a file has resolved to a DOI, the PRISM metadata publishers embed in
the PDF's XMP packet fills the gaps Crossref left: journal, volume, number,
pages and ISSN. Crossref stays authoritative — a field it answered is never
overwritten — so this only ever adds, and it adds exactly the bibliographic
detail the file itself is most likely to carry.

`-into` is the hand-off from a failed `fetch`: once the agent has the PDF,
it attaches that one file to the entry `fetch` left behind, after checking
that the file really is that paper. A file that fails the check stays
where it is. When the PDF is reachable at a URL, prefer `paper fetch
-into`, which downloads and attaches in one step and records where the
file came from; `ingest` is for files that are already on this machine.

```bash
$ paper ingest -since 2026-08-01T00:00:00 ~/Downloads/*.pdf
$ paper ingest -into hoeffding_1963 ~/Downloads/hoeffding.pdf
```

### `paper check [-online] [<key>...]`

Validates entries: JSON parses, the entry type is known, required fields for
that type are present, author/editor names parse, all fields transliterate
cleanly, DOI/arXiv IDs are well-formed, and page ranges use `--`. With no
arguments it also runs a store-wide fsck (key matches directory name,
referenced files exist on disk, no stray sync-conflict files) and promotes
any `draft` entry with zero errors to `clean`. Exits nonzero if any
error-severity problem was found.

`-online` additionally looks every DOI up at Crossref. A DOI that does not
resolve is an error — it is a typo or a hallucination. A title or year that
disagrees, or a lookup that fails for any other reason, is only a warning:
Crossref being unreachable or wrong must not condemn the entry.

```bash
$ paper check
$ paper check -online
```

### `paper search [-json] [-holdings <h>] [-status <s>] <terms>...`

Ranked metadata search over every `paper.json` in the store (matches on key,
author, title, year, journal/booktitle, DOI, arXiv ID, abstract). Draft
entries and deprecated versions are flagged in the output. `-json` prints
machine-readable results instead of one line per hit.

```bash
$ paper search -json voss
```

### `paper bib <key>... | -all`

Deterministic bibtex generation from `paper.json`, sorted by key and
separated by blank lines.

```bash
$ paper bib -all
```

### `paper audit [-online] [-json] <refs.bib>`

Checks a bibliography file — the kind an agent hands off after writing a
paper — against reality, without touching the store. A malformed entry
does not stop the run: it is reported with its line number and skipped,
and everything else is still audited. Every entry is
matched against the store by DOI, then arXiv ID, then title; a match
against a `clean` store entry is diffed field by field, and the store is
the authority for anything it holds, since a clean entry has already
passed `paper check`. A reference the store does not hold is always
checked against the network (Crossref, arXiv, zbMATH, DBLP); `-online`
does not gate that — it only adds re-verification of store-held entries
against their sources too. A DOI Crossref does not recognise is not
necessarily invented: Crossref only indexes Crossref-registered DOIs, so a
DataCite-registered one (Zenodo, figshare, many arXiv-issued DOIs) is
still confirmed if the DOI handle system resolves it, since that answers
existence independent of which registrar holds the metadata.

Each reference gets one of four verdicts: `confirmed` (matched a clean
store entry, or verified online), `unverified` (a source returned a
plausible but not clearly matching candidate), `notFound` (no source
returned anything — likely a hallucinated reference), or `unchecked` (not
looked up, e.g. because a source was down, so nothing was proved either
way). `-json` prints the full report as JSON instead of prose.

```
$ paper audit refs.bib
3 references: 1 confirmed, 1 unverified, 1 not found, 0 not checked

Not found:
ghost: no source returned any candidate — likely hallucinated

Unverified:
maybe:
  0.86  Jones, "Adaptive methods", 2011  [crossref]

Confirmed: hoef -> hoeffding_1963
```

## Agent rule

Every store mutation goes through a `paper` command or ends with a passing
`paper check`. Agents never free-form-manage the store: if `paper` cannot do
something directly, edit `paper.json` by hand and then run `paper check` to
validate the result.
