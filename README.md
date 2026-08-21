# paper

`paper` is a CLI tool for managing a local store of scientific papers with bibtex metadata. It provides a companion interface for Claude agents to organize, search, and manage research documents.

To install the latest version, run:

```bash
go install seehuhn.de/go/paper@latest
```

## The store

The store is a plain directory, in a location of your choosing: one
subdirectory per paper, named by its citation key, plus a config file, an
inbox drop zone and an event log.

```
~/Papers/
├── config.json            contact email for the online services
├── inbox/                 drop zone for PDFs awaiting ingest
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

### `config.json`

Store-wide settings, all optional; a missing file means an empty
configuration.

```json
{"email": "you@example.com"}
```

`email` is the contact address the online metadata services are given.
Unpaywall requires one and refuses to answer without it, and Crossref
uses it to put requests in its faster, better-behaved "polite pool".

### `events/`

One JSON line per command outcome, appended to `events/<hostname>.jsonl`:
what was run, what it was given, and how it ended. The log is
best-effort and local — a failure to write it never fails a command, and
each machine writes only its own file, so a synced store never sees a
conflict. No command reads it: it is kept so that what works in practice
can be looked at later.

### Store-root resolution

The store location is entirely user-configured; `paper` has no built-in
default. Every command that touches the store resolves its root in this
order:

1. the `-store` flag, if given;
2. the `PAPER_STORE` environment variable, if set.

If neither is set, the command fails with an error. The usual setup is to
export `PAPER_STORE` in your shell profile, pointing at a directory that
is synced between your machines (e.g. via syncthing).

## Commands

Six commands are implemented so far (`audit` is planned but not yet
built).

### `paper help`

Lists the available commands. Also shown when `paper` is run with no
arguments.

```bash
$ paper help
```

### `paper fetch [-dry-run] <ref>`

Resolves a reference online and downloads what can be fetched reliably by
script. The reference is a DOI, an arXiv ID or URL, or free text, which is
put through a Crossref search and accepted only when one hit clearly wins.
A DOI gets its metadata from Crossref and its PDF from Unpaywall, if there
is an open-access route; an arXiv ID gets the preprint's PDF and tex
source, with the published record merged in when the preprint names a DOI.
`-dry-run` reports what would happen and writes nothing.

The failures are hand-offs to the calling agent, not bare errors. Where no
download route exists the draft entry is still created, and the command
exits nonzero reporting what it learned about the paper, what the entry
holds, and where the file should go once found by hand. Free text that
pins down no single paper creates no entry at all, and the error lists the
candidates from Crossref, zbMATH and DBLP to choose between.

```bash
$ paper fetch 10.1080/01621459.1963.10500830
$ paper fetch arXiv:2412.05039
```

### `paper ingest [-since <ts>] [-into <key>] [-doi <doi>] [-arxiv <id>] <file.pdf>...`

Works out which paper each PDF already on disk is, and moves it into the
store. Identification is tiered: the Info dictionary first, then a DOI or
arXiv stamp in the page text, then the title read off the first page and
searched at Crossref. Nothing is downloaded — the file is already held, so
the services are asked for metadata only. `-since` drops files older than a
timestamp, and `-doi`/`-arxiv` say outright what a single file is, skipping
identification.

`-into` is the hand-off from a failed `fetch`: once the agent has found the
PDF by hand, it attaches that one file to the entry `fetch` left behind,
after checking that the file really is that paper. A file that fails the
check stays where it is.

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

## Agent rule

Every store mutation goes through a `paper` command or ends with a passing
`paper check`. Agents never free-form-manage the store: if `paper` cannot do
something directly, edit `paper.json` by hand and then run `paper check` to
validate the result.
