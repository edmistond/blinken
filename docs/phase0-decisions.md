# Phase 0: product spike findings

Date: 2026-09-10. Toolchain: Go 1.23.5 on macOS arm64.

Phase 0 had four goals. All four are met by the code in this repository.

| Goal | Result |
| --- | --- |
| Validate the CLI vocabulary with hand-written sessions | Done, see `docs/example-sessions.md`. Vocabulary held up; two small adjustments noted there. |
| Decide the CLI parser and ID format | Kong and ULID, evidence below. |
| Prove SQLite WAL behavior with concurrent writers | Proven in-process and across processes. One real race found and fixed, details below. |
| Prove one executable can embed and serve the browser UI | Proven: `blinken review --serve` serves `web/` from `embed.FS` on a random loopback port with host and token checks. |

These are recommendations with evidence, for the owner to confirm. The open
questions they address are Q1, Q2, Q4, and Q10 from the spec.

## Q1: ID format. Recommendation: ULID

Library: `github.com/oklog/ulid/v2` (no transitive dependencies).

- 26 characters, Crockford base32, no hyphens. A UUIDv7 is 36 characters
  with hyphens. In compact rows and shell pipelines the shorter form matters.
- Time-sortable to the millisecond, with monotonic entropy so IDs minted in
  the same millisecond by one process still sort in creation order.
- Case-insensitive on input, which is friendlier when a human retypes one.
- The spec's own examples (`01K...`) are ULIDs, so the format already matches
  the mental model. IDs minted today start with `01M`.
- UUIDv7 via `github.com/google/uuid` would also work, and Kong pulls that
  module in anyway, but nothing in Blinken benefits from UUID compatibility.

Prefix matching (`blinken show 01M26Q7`) is a plausible Phase 1 ergonomics
addition and works equally well with either format.

## Q2: CLI parser. Recommendation: Kong

Library: `github.com/alecthomas/kong` v1.16.1.

Measured against the criteria in the spec:

- **Help output.** Defaults render inline (`--confidence="medium"`), which
  satisfies the requirement that ranking defaults be visible in `--help`
  without extra code. Enum values validate at parse time with a clear error
  and usage exit code.
- **Ergonomics.** The whole command tree is a struct with tags. Repeatable
  flags (`--file`), env-var fallbacks (`--session` from `BLINKEN_SESSION`),
  optional positionals, and nested subcommands (`session start`) each took one
  tag. The Phase 0 CLI is one file.
- **Testability.** A parser is built from the struct and given an argument
  slice, so tests can drive it without `os.Args`.
- **Dependencies.** Kong brings `google/uuid` and `mattn/go-isatty`
  transitively. Cobra would bring `pflag` plus `mousetrap` and a generator
  package. The standard `flag` package has no subcommand support, no repeated
  flags without a custom type, and no default-in-help rendering, so it would
  need a few hundred lines of dispatch code to reach parity.
- **Maintenance.** Actively released through 2026.

Not chosen: Cobra (heavier, more ceremony, help output is fine but verbose),
urfave/cli (fine, but Kong's struct model is a better fit for a small
command set), standard `flag` (see above).

## Q4: input form

None of the four example sessions needed stdin or JSON input. Multiline
summaries pass through `--summary "$var"` cleanly. Recommendation: defer
structured input until an agent integration demonstrates a need.

## Q10: SQLite driver. Recommendation: modernc.org/sqlite

Three drivers were compared with an identical program: 16 writers, each with
its own connection pool, inserting 50 rows in short transactions, WAL mode,
5 s busy timeout, `BEGIN IMMEDIATE`. Stripped binaries, macOS arm64.

| Driver | CGO | Build (cold) | Binary | 800 inserts, 16 writers | Fresh-process open + 1 insert |
| --- | --- | --- | --- | --- | --- |
| `modernc.org/sqlite` v1.38.2 | no | 0.2 s (cached) | 6.0 MB | 545 to 650 ms | 7 ms |
| `mattn/go-sqlite3` v1.14.32 | yes | 11.9 s | 3.4 MB | 96 to 216 ms | 5 ms |
| `ncruces/go-sqlite3` v0.27.1 | no | 1.7 s | 6.8 MB | 50 to 52 ms | 423 ms |

Reading the table:

- **Per-process startup dominates for a CLI.** Every `blinken guess` is a
  new process that opens the database once and writes one row. The ncruces
  driver is fastest at steady-state throughput but pays ~420 ms per process
  to compile its WebAssembly SQLite build. That is disqualifying for the
  write path unless its compilation cache is configured, which adds a cache
  directory and failure modes to manage.
- **modernc and mattn are indistinguishable at the scale that matters.**
  Under 1 ms per insert for either; a 2 ms startup difference.
- **CGO is the real cost of mattn.** A binary built with `CGO_ENABLED=0`
  compiles but panics on first use (`go-sqlite3 requires cgo to work`).
  Cross-compiling working binaries for Linux and Windows would need a C
  cross-toolchain in the release pipeline. modernc cross-compiles to
  linux/amd64 with a plain `GOOS=linux go build`.
- **Binary size** favors mattn by 2.6 MB. The spec says size alone should
  not decide, and it does not here.

modernc.org/sqlite is the choice: pure Go, trivial cross-compilation,
startup latency equal to the C driver, and the most widely deployed pure-Go
option. Its transpiled C is the least readable of the three codebases, which
is acceptable because Blinken uses it only through `database/sql`.

Connection settings that matter, all set in the DSN so every connection gets
them: `busy_timeout(5000)`, `journal_mode(WAL)`, `synchronous(NORMAL)`,
`foreign_keys(1)`, `_txlock=immediate`.

## Concurrent writers: what was proven and what was found

**In-process test** (`internal/storage`): 16 goroutines, each with its own
`*sql.DB` on the same file, 50 inserts each. 800 rows, zero errors, roughly
half a second.

**Cross-process script** (`scripts/concurrent-writers.sh`): 12 to 32
separate `blinken guess` processes started at once against one database.

The cross-process run exposed a race the in-process test could not: several
processes opening a brand-new database file in the same instant produced
`SQLITE_BUSY` from the schema bootstrap, roughly one run in eight. Two causes,
two fixes:

1. The bootstrap `CREATE TABLE IF NOT EXISTS` ran in autocommit mode. That
   statement starts as a read and upgrades to a write, and SQLite returns
   `SQLITE_BUSY` immediately on that upgrade path without invoking the busy
   handler, to avoid deadlock. Fix: run the bootstrap inside an explicit
   `BEGIN IMMEDIATE` transaction so the write lock is taken up front.
2. A rarer residual failure on the very first WAL conversion of a fresh
   file, which the busy handler also does not cover. Fix: a bounded jittered
   retry (about 2 s total) around the bootstrap only.

Evidence after the fixes: 0 failures in 20 fresh-file runs of 16 processes,
and 0 failures in 12 runs against a pre-created database, where the race
cannot occur. Ordinary steady-state writes never needed the retry; it exists
for the one-time first open.

Take-away for Phase 1: keep the multi-process script in CI. It caught what
the goroutine test missed.

## Embedded browser UI

`web/embed.go` embeds `index.html` and `static/` with `//go:embed`.
`internal/server` serves them on `127.0.0.1:0` with:

- a random per-run token injected into the page and required on every
  non-GET request (CSRF);
- rejection of any `Host` other than localhost, 127.0.0.1, or ::1 (DNS
  rebinding);
- `Content-Security-Policy: default-src 'self'` and `nosniff`;
- explicit read, write, and idle timeouts, and graceful shutdown on
  SIGINT or SIGTERM.

The page renders all user text through `textContent`. Tests cover asset
presence, loopback binding, token injection, host and token rejection, and a
status update round-trip into the database. The Crit repository confirmed the
same shape: a `web/` package with `embed.go`, served from the same process
that owns the data.

## Things the spike deliberately did not do

- Bulk `--all` review actions, `delete`, `purge`, `sessions` listing,
  config files, redaction, and `NO_COLOR` handling. All Phase 1.
- Browser keyboard shortcuts and filters. Phase 1.
- Any release tooling.

## Guesses recorded while building this

In the spirit of the tool, the choices made here without a clear answer in
the spec:

- Module path `github.com/edmistond/blinken`, inferred from the author's
  email. Cheap to change before anything imports it.
- The `guesses` summary line gained a `followup` count. The spec's example
  shows three fields.
- `--chosen` is the flag name for `chosen_behavior`.
- Compact rows put impact before confidence (`C/L`). The spec example shows
  `H/L` without saying which letter is which; impact first puts the more
  decision-relevant label in the leading position.
- Exit codes: 2 usage, 3 not found, 4 storage, 1 unexpected.
