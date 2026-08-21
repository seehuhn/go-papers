# paper

`paper` is a CLI tool for managing a local store of scientific papers with bibtex metadata. It provides a companion interface for Claude agents to organize, search, and manage research documents.

To install the latest version, run:

```bash
go install seehuhn.de/go/paper@latest
```

## The store

The store is a plain directory, in a location of your choosing: one
subdirectory per paper, named by its citation key, plus a config file and
an inbox drop zone.

```
~/Papers/
├── config.json            contact email for API polite pools, defaults
├── inbox/                 drop zone for PDFs awaiting ingest
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

Four commands are implemented so far (`fetch`, `ingest`, and `audit` are
planned but not yet built).

### `paper help`

Lists the available commands. Also shown when `paper` is run with no
arguments.

```bash
$ paper help
```

### `paper check [<key>...]`

Validates entries: JSON parses, the entry type is known, required fields for
that type are present, author/editor names parse, all fields transliterate
cleanly, DOI/arXiv IDs are well-formed, and page ranges use `--`. With no
arguments it also runs a store-wide fsck (key matches directory name,
referenced files exist on disk, no stray sync-conflict files) and promotes
any `draft` entry with zero errors to `clean`. Exits nonzero if any
error-severity problem was found.

```bash
$ paper check
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
