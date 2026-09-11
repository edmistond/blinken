# Blinken

Blinken is a local-first Go CLI that lets coding agents record consequential guesses for human review.

## Specification

- `spec.html` is the canonical specification. Read it first; it is the easier-to-read rendering of the plan.
- `spec.md` is the original markdown draft and is retained for history. If the two disagree, `spec.html` wins.
- When the plan changes, update `spec.html`. Keep it self-contained (the logo is embedded as a data URI).

## Working conventions

- Checkpoint every turn or major change as a git commit so work can be rolled back.
- Never run `purge` against the default database; point destructive tests at `--db /tmp/...`.
- `blinken.png` is the project logo.

## Recording guesses

This project dogfoods its own tool. When you must choose between materially different behaviors and the spec, tests, or existing code do not determine the answer, record it before moving on:

```bash
blinken guess "what you chose" --would-ask "what you would have asked"
```

Run `export BLINKEN_SESSION=$(blinken session start --agent <agent> --model <model>)` once at the start of your work so guesses group by session. Add `--kind`, `--confidence`, `--impact`, `--reversibility`, `--ambiguity`, `--reason`, `--alternative`, and `--file` when the choice is consequential. Record at the moment of the decision, not at the end. See `skills/blinken/SKILL.md` for what counts as a guess and how to rate one.

## Layout

- `cmd/blinken` is the thin composition root (Kong CLI).
- `internal/core` holds models, validation, ranking, project detection. No I/O.
- `internal/storage` is SQLite via `modernc.org/sqlite` (pure Go) with ordered migrations.
- `internal/server` is the loopback-only review server.
- `web/` holds the browser UI, embedded with `//go:embed`.
- `docs/` holds the output-format contract, example sessions, and Phase 0 decisions.
- `skills/blinken/SKILL.md` is the reference guidance for coding agents on when and how to record a guess.

## Build and test

```bash
go build ./... && go test ./...
go build -o blinken ./cmd/blinken
./scripts/concurrent-writers.sh   # multi-process SQLite stress test
```

Go 1.23.5 is the toolchain in use. Stop and tell the user before pulling any dependency that needs a newer Go.
