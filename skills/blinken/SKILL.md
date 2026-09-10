---
name: blinken
description: Record a guess with blinken whenever you must choose among materially different outcomes and the spec, tests, docs, or project conventions do not clearly determine the answer. Use at the moment of the decision, not at the end of the run.
---

# Recording guesses with blinken

Blinken is a local ledger of the consequential places where you had to guess.
A human reviews the ledger later and accepts, rejects, or follows up on each
guess. Your job is to make that review possible by recording the guess when
it happens, cheaply, with enough context to judge it.

## When to record

Record a guess when all three hold:

1. You had to choose among materially different behaviors or approaches.
2. The specification, tests, documentation, code, or established conventions
   did not clearly determine the answer.
3. The choice could matter to the user, product, system, data, security,
   maintainability, or externally visible behavior.

Worth recording:

- unspecified error, retry, fallback, or persistence behavior
- destructive or data-retention semantics
- security and authorization assumptions
- compatibility and externally visible API decisions
- inferred business rules and ambiguous acceptance criteria
- deviations from what was asked
- architecture choices where requirements do not distinguish the alternatives

Usually not worth recording: variable names, formatting, mechanical
refactors, obvious language idioms, anything fully determined by existing
project conventions.

## How to record

Start a session once at the beginning of your run so guesses group together:

```bash
export BLINKEN_SESSION=$(blinken session start --agent <your-name> --model <model>)
```

Then, at the moment you decide, one command. The minimal form is enough:

```bash
blinken guess "Suspended users keep read access to existing files"
```

The command prints only the new id. Do not read anything back.

When the choice is consequential, spend a few more flags. Each one is a
field the reviewer will see:

```bash
blinken guess \
  --kind inference \
  --confidence low --impact high --reversibility hard \
  --summary "Suspended users keep read access to existing files" \
  --ambiguity "Spec restricts writes for suspended users but says nothing about reads" \
  --reason "Neighboring handlers treat suspension as write-disabled only" \
  --alternative "Block all file access on suspension" \
  --would-ask "Should suspension hide existing files entirely?" \
  --file src/files/download.go:118
```

An ideal guess says what was ambiguous, what you chose, why, what else was
plausible, and what you would have asked if asking were free. The
`--would-ask` line is the single most useful thing you can give a reviewer.

## Rating the guess

- `--confidence` low, medium, high: how sure you are the choice is right.
- `--impact` low, medium, high, critical: how much it matters if wrong.
  Anything touching deletion, money, security, or an external contract is
  high or critical even if you feel confident.
- `--reversibility` easy, moderate, hard: how costly it is to change later.
  A database schema or a published API is hard; a default value is easy.
- `--kind`: assumption (spec omitted it), interpretation (spec supported
  more than one reading), tradeoff (several viable approaches), deviation
  (you did something other than what was asked), inference (you derived
  intent from code, tests, or conventions).

Omitted ratings default to medium, medium, moderate. Those defaults are
labels, not measurements, so set them when you have a view.

## Before you finish

Do not reconstruct a ledger at the end; the record is only trustworthy if it
was written when the decision was made. If you did skip one, record it now
with a lower confidence and say so in `--reason`.

You may run `blinken guesses` to print a one-line count for your summary to
the user, and `blinken guesses --compact` to list what you recorded.

## Reading prior decisions

Before redoing something a previous run decided, check whether a human has
already ruled on it:

```bash
blinken guesses --all --compact
blinken show <id>
```

A `rejected` status with a note is an instruction: the human wants the
other behavior.
