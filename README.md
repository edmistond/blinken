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

## Using blinken with your agent

Installing the skill is not enough on its own. An agent notices a good
moment to record a guess only if a standing instruction tells it to, so
there are three pieces, in order of importance.

**1. A standing instruction** in the file your agent always reads:
`CLAUDE.md`, `AGENTS.md`, or equivalent. A few lines are enough:

```markdown
## Recording guesses
When you must choose between materially different behaviors and the spec,
tests, or code do not determine the answer, record it before moving on:
    blinken guess "what you chose" --would-ask "what you would have asked"
Run `export BLINKEN_SESSION=$(blinken session start --agent claude-code)` once
at the start of your work. See the blinken skill for when a guess is worth
recording and how to rate one.
```

This repository's own [AGENTS.md](AGENTS.md) is a worked example.

**2. The skill**, which carries the detail the instruction leaves out: the
three-part test for what counts, the rating scales, and the rich form of the
command. For Claude Code, copy or symlink it into the personal or project
skills directory:

```bash
ln -s "$(pwd)/skills/blinken" ~/.claude/skills/blinken        # every project
ln -s "$(pwd)/skills/blinken" /path/to/repo/.claude/skills/blinken   # one project
```

Agents that read `AGENTS.md` but have no skill system still get the standing
instruction, which is the part that matters most.

**3. A hook that starts the session for you**, so the agent never has to
remember that step. Claude Code runs `SessionStart` hooks in the project
directory, and any `export` lines a hook writes to the file named by
`CLAUDE_ENV_FILE` apply to every later shell command in that session. In
`.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo \"export BLINKEN_SESSION=$(blinken session start --agent claude-code)\" >> \"$CLAUDE_ENV_FILE\""
          }
        ]
      }
    ]
  }
}
```

With the hook in place, every `blinken guess` the agent runs is attributed to
that session automatically, and `blinken review --serve` can filter by it.

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
