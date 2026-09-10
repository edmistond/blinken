<img src="blinken.png" alt="Blinken: a robot in a feathered cap and dark glasses" width="180" align="right">

# Blinken

Show me where the agent made the project up.

Blinken is a local-first command-line tool that lets coding agents record the
consequential places where they had to guess, because the spec, docs, tests,
or existing code did not determine an answer. It ranks those guesses for
review and makes it easy to accept, reject, or follow up on each one.

No service, no account, no network. One Go binary and a SQLite file in your
user data directory.

## Install

```bash
go install github.com/edmistond/blinken/cmd/blinken@latest
```

Or clone and `go build -o blinken ./cmd/blinken`.

## Use

An agent records a guess at the moment it makes one:

```bash
blinken guess "Suspended users keep read access to existing files"
# 01M26Q7913RGE7ZJZHAYV8KT00
```

A human reviews later, highest priority first:

```bash
blinken guesses          # 3 unreviewed | 0 followup | 1 high-impact | 1 low-confidence
blinken review           # verbose listing in the terminal
blinken review --serve   # browser UI on a random loopback port
blinken reject 01M26Q79 --note "Suspension must block reads too"
```

Ids accept any unique prefix. See `blinken --help` and
[docs/output-format.md](docs/output-format.md) for the full contract, and
[skills/blinken/SKILL.md](skills/blinken/SKILL.md) for the guidance to give
an agent.

## Configuration

Optional, at `~/Library/Application Support/blinken/config.toml` on macOS,
`$XDG_CONFIG_HOME/blinken/config.toml` on Linux, `%AppData%\blinken\config.toml`
on Windows, or wherever `BLINKEN_CONFIG` points:

```toml
[recording]
capture_cwd = true
capture_files = true

[privacy]
redact = ["literal-sensitive-value"]
redact_patterns = ["ticket-\\d+"]
ignore_paths = [".env", "secrets/**"]
builtin_secret_heuristics = true

[review]
open_browser = true
include_followup = true
```

Redaction runs before anything is written. The built-in heuristics catch
common credential shapes and are not a complete secret scanner.

## Privacy

The database is sensitive local data: it holds whatever free text agents
record, plus working directories and file paths. Blinken never captures
environment variables, command output, or source-file contents. `delete`
soft-deletes; `purge --hard` removes soft-deleted rows permanently. There is
no telemetry.

## Development

```bash
go test ./...
./scripts/concurrent-writers.sh
```

The specification lives in `spec.html`. Phase 0 findings are in
`docs/phase0-decisions.md`.

MIT license.
