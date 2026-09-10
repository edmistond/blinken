# Example sessions

Hand-written walkthroughs used to validate the CLI vocabulary before building
more of it. Output shown is what the Phase 0 spike actually prints.

## Session 1: an agent implements a feature

A coding agent is asked to "add account suspension" to a web service. The
spec says suspended users cannot write. It says nothing about reading.

```bash
export BLINKEN_SESSION=$(blinken session start --agent claude-code --model claude-fable-5-1)
```

Twenty minutes in, the agent hits the read-access question and records the
guess at that moment, then keeps working:

```bash
blinken guess --kind inference --confidence low --impact critical --reversibility hard \
  --summary "Suspended users may still download existing files" \
  --ambiguity "The spec defines write restrictions but not read access" \
  --reason "Neighboring handlers treat suspension as write-disabled" \
  --alternative "Block all file access" \
  --would-ask "Should suspension make existing files inaccessible?" \
  --file src/files/download_handler.go:118
```

```
01M26Q791CJ5GHV499HZACF303
```

Later it makes two smaller calls and records them with the minimal form,
accepting the medium/medium/moderate defaults:

```bash
blinken guess "Failed payment webhooks retry once, then alert"
blinken guess --kind tradeoff --impact low --reversibility easy "Suspension reason stored as free text, not an enum"
blinken session end
```

Cost to the agent: three short commands, three ULIDs back. No JSON to
construct, nothing to read.

## Session 2: the human reviews

```bash
blinken guesses
```

```
3 unreviewed | 0 followup | 1 high-impact | 1 low-confidence
```

That one high-impact, low-confidence item is what to look at first.

```bash
blinken review
```

```
01M26Q791CJ5GHV499HZACF303  unreviewed  low confidence, critical impact, hard to reverse
  Suspended users may still download existing files
  kind:        inference
  ambiguity:   The spec defines write restrictions but not read access
  reason:      Neighboring handlers treat suspension as write-disabled
  alternative: Block all file access
  would ask:   Should suspension make existing files inaccessible?
  cwd:         /Users/dev/service
  files:       src/files/download_handler.go:118
  session:     01M26Q790S57TQJVGEGQ2WNDK0
  created:     2026-09-10 18:32

01M26Q7913RGE7ZJZHAYV8KT00  unreviewed  medium impact, moderate to reverse
  Failed payment webhooks retry once, then alert
  ...
```

The reviewer disagrees with the first guess and wants the retry behavior
confirmed with someone else:

```bash
blinken reject 01M26Q791CJ5GHV499HZACF303 --note "Suspension must block all file access, including reads"
blinken followup 01M26Q7913RGE7ZJZHAYV8KT00 --note "Ask payments team about retry policy"
blinken accept 01M26Q791MJGYVY6VRGSQ2084D
blinken guesses
```

```
0 unreviewed | 1 followup | 0 high-impact | 0 low-confidence
```

The rejection note is the human's decision. Blinken does not change any code.
The agent's next run can read it with `blinken show <id>` and act on it.

## Session 3: browser review after a long run

An overnight run left 40 guesses. The terminal list is too long to read.

```bash
blinken review --serve
```

```
http://127.0.0.1:55959
```

The browser shows counts, then the queue sorted by priority with the
three labels and the reason on each row. The reviewer accepts the low-impact
items from the page, adds notes to two, and closes the tab. Ctrl-C stops the
server. State persisted through the same database, so `blinken guesses` in
the terminal reflects the browser actions immediately.

## Session 4: an agent reads its own history

A second agent picks up the project the next day and wants to know which
decisions it should not silently redo:

```bash
blinken guesses --all --compact
```

```
01M26Q791CJ5GHV499HZACF303 C/L inference      Suspended users may still download existing files
01M26Q7913RGE7ZJZHAYV8KT00 M/M -              Failed payment webhooks retry once, then alert
01M26Q791MJGYVY6VRGSQ2084D L/M tradeoff       Suspension reason stored as free text, not an enum
```

```bash
blinken show 01M26Q791CJ5GHV499HZACF303
```

The `rejected` status and the human note tell it exactly what to change.

## Observations from writing these

What worked:

- `guess` as the verb reads naturally in every session. `blinken guess "..."`
  is the whole write path; nobody needs `--kind` to get value.
- The `--would-ask` flag is the most useful field for the reviewer. It turns
  a record into a question the human can answer with one word.
- `accept` / `reject` / `followup` as top-level verbs are shorter than
  `blinken review accept <id>` and read as decisions.
- Compact rows are scannable and cheap: an agent can pull 40 rows for a few
  hundred tokens.
- ULIDs match the `01K...` shape the spec already used in examples, paste
  cleanly into shells, and sort by time in `--compact` output without a
  date column.

What felt off, and what the spike does about it:

- The spec's one-line summary omits `followup`. A reviewer who flagged three
  items yesterday wants to see them counted, so the spike prints
  `N followup` as a fourth field.
- `blinken guesses` and `blinken review` overlap. In practice `guesses` is
  the agent's read command (counts, compact, JSON) and `review` is the
  human's (verbose, or `--serve`). Keeping both is fine as long as the help
  text says so.
- `session end` with no argument needs `BLINKEN_SESSION`. The spike accepts
  the env var and errors clearly if it is unset.
- A `--chosen` flag exists for `chosen_behavior`, but none of the sessions
  needed it. It stays because it is cheap, but the reference skill should
  not push agents toward it.
- `--file` repeatable works. Agents did not miss a `--files a,b,c` form.
- Nothing in these sessions needed stdin or JSON input (open question Q4).
  Option-based recording covered every case, including a multiline
  `--summary` passed through a shell variable.
