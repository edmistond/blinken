# Output format contract

Agent-facing output is compact, deterministic text. These shapes are stable
across releases; changes are breaking changes.

## Write commands

`guess`, `accept`, `reject`, `followup`, `session start`, `session end` print
exactly one line on success: the affected identifier. `--quiet` prints nothing.

Identifiers are ULIDs: 26 characters, Crockford base32, time-sortable,
case-insensitive on input.

## `guesses` (default)

One line of counts for the current project:

```
8 unreviewed | 1 followup | 2 high-impact | 3 low-confidence
```

`high-impact` counts unresolved guesses with impact `high` or `critical`.
`low-confidence` counts unresolved guesses with confidence `low`.
Unresolved means status `unreviewed` or `followup`.

## `guesses --compact`

One row per guess, highest review priority first:

```
ID                         I/C KIND           SUMMARY
01M26Q791CJ5GHV499HZACF303 C/L inference      Suspended users may download existing files
01M26Q7913RGE7ZJZHAYV8KT00 M/M -              Suspended users retain read-only access
```

| Column | Meaning |
| --- | --- |
| `ID` | ULID |
| `I/C` | impact letter / confidence letter. Impact: `L` low, `M` medium, `H` high, `C` critical. Confidence: `L` low, `M` medium, `H` high. |
| `KIND` | kind, or `-` when not supplied; padded to 14 columns |
| `SUMMARY` | summary text, unwrapped |

Reversibility is not in the compact row. It is visible in `show`, `review`,
and JSON.

## Priority reason

`show` and `review` lead with the status and the plain-language reason:

```
low confidence, critical impact, hard to reverse
```

"low confidence" appears only when confidence is low. Impact always appears.
Reversibility appears as "easy to reverse", "moderate to reverse", or "hard to
reverse". The numeric score is available only in `--json`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | unexpected failure |
| 2 | usage error |
| 3 | record not found |
| 4 | storage error |

Diagnostics and errors go to stderr, prefixed `blinken:`.
