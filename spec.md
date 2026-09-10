# Blinken: Initial Engineering Specification

Status: Draft for interrogation and refinement  
Target: Initial open-source implementation  
Primary implementation language: Go  
License: MIT  

## 1. Summary

`blinken` is a local-first command-line tool that lets coding agents record the consequential places where they had to guess because the specification, documentation, tests, or existing code did not clearly determine an answer.

Its purpose is not to preserve a complete agent transcript. A long autonomous run may produce tens of thousands of lines of activity but only a few dozen decisions that materially determine whether the result is right. Blinken captures those decisions when they happen, ranks them for human review, and makes it comfortable to accept them, reject them, or mark them for follow-up.

Blinken is observability for agent judgment:

> Show me where the agent made the project up.

The name may retain a small amount of personality, and the user-facing noun should be **guess**. Internally, the implementation may use more neutral names such as `GuessRecord` or `DecisionRecord` where that improves clarity.

## 2. Problem statement

Code review alone becomes less sufficient as coding agents work for longer periods. An implementation can be locally coherent and mechanically correct while being founded on an early, consequential interpretation that the user never intended.

Agents are also poor witnesses to their own earlier reasoning. Asking for a reconstructed ledger at the end of a long run is likely to produce an incomplete or overly tidy account. Blinken should therefore make recording a guess cheap enough that an agent can do it at the moment the ambiguity is encountered.

A record is useful when all of the following are true:

- The agent had to choose among materially different behaviors or approaches.
- The available specification, documentation, tests, code, or conventions did not clearly determine the answer.
- The choice could matter to the user, product, system, data, security, maintainability, or externally visible behavior.

Routine implementation details do not belong in Blinken.

## 3. Goals

Blinken should:

- Record consequential guesses during an agent run with very little invocation and output overhead.
- Work without a hosted service, account, API key, or network connection.
- Store records durably in a user-level SQLite database shared by the user's local agent sessions.
- Detect the current project or repository and capture the working directory at the time of the guess.
- Associate guesses with logical agent sessions when possible.
- Rank unresolved guesses by review value, considering confidence, impact, and reversibility.
- Provide both a compact CLI workflow and a comfortable local browser review workflow.
- Let a human mark guesses as accepted, rejected, or requiring follow-up, with optional notes.
- Preserve enough context to understand a guess without becoming a transcript or source-code archive.
- Play well with RTK and other token-saving command wrappers by producing small, deterministic output.
- Leave room for optional, local semantic similarity without requiring LLM calls or token spend.
- Remain useful at MVP scale without embeddings, clustering, a daemon, or any cloud component.

## 4. Non-goals

The initial product is not:

- An agent orchestrator.
- A complete transcript, tool-call, or command-output recorder.
- A general logging or telemetry platform.
- A source-code review system.
- A task tracker.
- A replacement for project documentation, tests, specifications, or ADRs.
- A system that automatically changes source code after a guess is rejected.
- A cloud synchronization service.
- An LLM-based summarizer or classifier.
- A vector database.
- An editor plugin, MCP server, hosted dashboard, or always-running daemon.

Those may be explored later only if real usage demonstrates a need.

## 5. Design principles

### 5.1 Local first

Core recording, storage, querying, ranking, and review must work fully offline. No record should leave the machine as part of normal operation.

### 5.2 Cheap at the moment of uncertainty

The most important write path should be a single short command. Optional metadata should improve a record but should not make recording burdensome.

### 5.3 Human judgment remains authoritative

Scores and similarity matches are review aids. Blinken must not silently decide that two guesses mean the same thing, nor treat a model-generated confidence value as calibrated truth.

### 5.4 Agent output and human output serve different needs

Agent-facing write and query commands should default to compact, deterministic text. Human review may be verbose and should prefer legibility over token economy.

### 5.5 Boring, replaceable internals

Use ordinary Go and SQLite patterns. Add abstraction only around dependencies that are genuinely likely to change, especially local embedding generation. Do not build a ceremonial clean architecture.

## 6. Terminology

### Guess

An exact record of a consequential choice made under ambiguity. Every guess has its own immutable identifier even if it is later grouped with related guesses.

### Kind

Optional metadata describing the nature of the guess. The initial taxonomy is:

- `assumption`: the specification omitted something and the agent supplied an answer.
- `interpretation`: the available instructions reasonably supported more than one meaning.
- `tradeoff`: multiple approaches were viable and the agent deliberately chose one.
- `deviation`: the requested behavior could not or should not be followed exactly, so the agent did something else.
- `inference`: the agent derived intended behavior from code, tests, documentation, or conventions.

The CLI should still speak primarily in terms of guesses. Kind is metadata, not a collection of competing top-level user concepts.

### Session

One logical coding-agent run. Sessions provide grouping and provenance but should not be mandatory for recording a guess.

### Project

The local repository or project to which a guess belongs. Project scope is used for review, filtering, and later similarity comparison.

### Subject

A human-confirmed grouping of guesses that concern the same underlying ambiguity. A subject gets a durable identifier only after grouping. Agents should not be expected to invent stable subject keys independently.

### Review status

The state of human review:

- `unreviewed`
- `accepted`
- `rejected`
- `followup`

`superseded` may be added later if real workflows require it.

## 7. Primary workflows

### 7.1 Start a session

```bash
blinken session start --agent codex --model gpt-5.6
```

Default output:

```text
01K...
```

The caller can export the returned identifier:

```bash
export BLINKEN_SESSION=01K...
```

An explicit `--session` option takes precedence over `BLINKEN_SESSION`.

Session management is optional. If no session is supplied, v1 may either leave `session_id` null or create a clearly identified implicit session. It must not use fragile process-tree heuristics.

### 7.2 Record a guess

Minimal form:

```bash
blinken guess "Suspended users retain read-only access"
```

Default output:

```text
01K...
```

Rich form:

```bash
blinken guess \
  --kind inference \
  --confidence medium \
  --impact high \
  --reversibility hard \
  --summary "Suspended users may download existing files" \
  --ambiguity "The specification defines write restrictions but not read access" \
  --reason "Neighboring handlers treat suspension as write-disabled" \
  --alternative "Block all file access" \
  --would-ask "Should suspension make existing files inaccessible?" \
  --file src/Files/DownloadHandler.cs:118
```

The positional argument maps to `summary`. `--summary` is useful for scripts and richer multiline input. Agents should not need to construct JSON to record a normal guess.

Recording should infer, unless overridden or disabled:

- project root and project identity
- current working directory
- current time
- session from `--session` or `BLINKEN_SESSION`

It should not inspect source files or capture their contents.

### 7.3 Inspect compactly

```bash
blinken guesses
```

Suggested default output:

```text
8 unreviewed | 2 high-impact | 3 low-confidence
```

Compact rows are available when the caller needs individual records:

```bash
blinken guesses --compact
```

```text
01K... H/L assumption Suspended accounts retain downloads
01K... M/M inference  Failed payments retry once
```

The precise abbreviations must be documented and stable. `--json` is available where structured output is genuinely useful, but compact text is the preferred agent-facing format because JSON often costs more tokens.

### 7.4 Review in the terminal

```bash
blinken review
```

Default scope:

- current project
- `unreviewed` and `followup` guesses
- highest review priority first

Including `followup` is a configurable default. The terminal view should show the factors behind the ordering rather than only an opaque score. Useful filters should include project, session, status, kind, confidence, impact, and time range.

### 7.5 Review in a browser

```bash
blinken review --serve
```

This starts an ephemeral local web server, binds only to loopback, chooses an available port by default, and opens the user's browser unless `--no-open` is supplied.

Example output:

```text
http://127.0.0.1:43817
```

The process remains in the foreground until interrupted. There is no separate daemon.

### 7.6 Resolve a guess

```bash
blinken accept 01K...
blinken reject 01K... --note "Suspended accounts must have no file access."
blinken followup 01K... --note "Confirm with product before release."
blinken accept --all --status unreviewed --impact low
```

Review status persists across review sessions. The CLI and browser UI should support bulk accept, reject, or follow-up actions over an explicitly selected set or visible filtered result. Bulk actions must show the number of affected guesses and require confirmation unless `--yes` is supplied.

These commands update review state only. They do not modify project files.

## 8. CLI surface

The proposed v0.1 command set is:

```text
blinken guess [text] [options]
blinken guesses [filters] [--compact|--json]
blinken show <guess-id> [--json]
blinken review [filters] [--serve] [--no-open]
blinken accept <guess-id> [--note text]
blinken reject <guess-id> [--note text]
blinken followup <guess-id> [--note text]
blinken accept|reject|followup --all [filters] [--note text] [--yes]
blinken session start [--agent name] [--model name] [--label key=value]
blinken session end [session-id]
blinken sessions [--compact|--json]
```

An alias such as `blinken record` may be retained for discoverability, but `guess` is the preferred user-facing command. Do not add separate `infer`, `tradeoff`, and `deviate` commands in v0.1; `--kind` is sufficient.

Cross-command conventions:

- Successful write commands print only the affected identifier by default.
- `--quiet` prints nothing on success.
- `--json` uses a documented schema and writes JSON only to stdout.
- Diagnostics and errors go to stderr.
- Exit code `0` means success; nonzero codes are stable enough for scripts to distinguish usage, missing-record, storage, and unexpected failures.
- Color and decorative glyphs must not be required to interpret output.
- Respect `NO_COLOR` and disable decoration when output is redirected.
- Avoid progress animation for operations expected to finish quickly.

RTK compatibility should come from economical normal CLI behavior, not a Blinken-specific RTK plugin.

## 9. Guess model

The logical `Guess` record contains:

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Time-sortable opaque ID such as UUIDv7 or ULID; choose one consistently. |
| `project_id` | yes | Resolved project identity. |
| `session_id` | no | Logical agent session. |
| `created_at` | yes | UTC timestamp. |
| `updated_at` | yes | UTC timestamp for review metadata changes. |
| `summary` | yes | Short human-readable statement of the choice. |
| `ambiguity` | no | What the available information failed to determine. |
| `chosen_behavior` | no | More detail than the summary when needed. |
| `kind` | no | Small taxonomy defined above. |
| `confidence` | yes | `low`, `medium`, or `high`; defaults to `medium`. |
| `impact` | yes | `low`, `medium`, `high`, or `critical`; defaults to `medium`. |
| `reversibility` | yes | `easy`, `moderate`, or `hard`; defaults to `moderate`. |
| `reason` | no | Evidence or rationale for the choice. |
| `alternative` | no | A plausible competing choice. |
| `would_ask` | no | What the agent would have asked if asking were free. |
| `cwd` | no | Working directory captured at record time. |
| `files` | no | Zero or more explicit file and optional line references. |
| `status` | yes | Defaults to `unreviewed`. |
| `review_note` | no | Human note associated with the current review state. |
| `reviewed_at` | no | UTC timestamp. |
| `deleted_at` | no | UTC timestamp for soft deletion; deleted guesses are hidden by default. |
| `subject_id` | no | Assigned only by explicit grouping. |
| `metadata_json` | no | Bounded extension metadata; not a substitute for core columns. |

The CLI must make ranking defaults visible in `--help` and must not imply that omitted agent judgments are measured facts. If it remains simple, output may distinguish caller-supplied values from defaults.

Review actions update the current status, note, and timestamp in place, and that state persists across review sessions. A separate immutable review-event log is not required for v0.1. Bulk actions use the same state transition as individual actions.

## 10. Projects and repository detection

Blinken stores data in a user-level database, not inside each repository.

Starting at the current working directory, project detection should walk upward for explicit and common markers:

1. An explicit project root supplied by the caller.
2. An optional Blinken project marker or configuration file.
3. A Jujutsu or Git repository boundary.
4. A reasonable fallback root for a non-repository project.

The implementation must handle Git worktrees and Jujutsu repositories without assuming that `.git` is always a directory.

For v0.1, the least invasive identity strategy is preferred:

- Generate a stable opaque `project_id` in the user database.
- Maintain aliases from observed canonical roots to that project.
- Store a display name separately.
- Do not require creation or commitment of a `.blinken` file.
- Do not derive identity solely from a remote URL, which may be absent, private, or change.

Moving a repository may initially create a new observed alias or require an explicit project-link command. Perfect move detection is not an MVP requirement.

Capture `cwd` separately from the project root. Prefer storing both the canonical absolute path and, when applicable, a computed project-relative path so later structural-locality experiments do not depend on string surgery. Users must be able to disable or redact path capture.

## 11. Sessions

A session records:

- `id`
- optional `project_id`
- start and optional end timestamps
- optional agent name
- optional model name
- initial working directory
- bounded key/value labels

Session identifiers are printed in a form that is easy to pass through environment variables and shell scripts.

Blinken must support concurrent sessions writing to the same database. Session end is advisory; abandoned open sessions are valid and must not block review.

## 12. Review priority

Confidence alone is not an adequate ordering. Low-confidence naming choices may be harmless, while a reasonably confident decision about deletion, security, money, or an external contract may deserve immediate review.

Conceptually:

```text
review priority = uncertainty × impact × difficulty of reversal
```

The initial implementation should use a small, deterministic heuristic:

```text
uncertainty:  low confidence = 3, medium = 2, high = 1
impact:       low = 1, medium = 2, high = 3, critical = 5
reversibility: easy = 1, moderate = 2, hard = 3
```

A simple product score is acceptable for v0.1. Ties should be deterministic, for example by impact and then creation time.

The score is a sorting aid, not a scientific measurement. Review output should prominently display the three input labels and a plain-language reason such as `critical impact, hard to reverse`. The raw number may be available in JSON but should not dominate the human UI.

Ranking rules must live in one testable component shared by CLI and browser views.

## 13. Storage

Use SQLite through Go's `database/sql` package and a small, well-supported SQLite driver. Prefer a driver that preserves straightforward single-binary, cross-platform builds, and document whether it requires CGO before committing to it.

Default database locations should follow operating-system conventions:

```text
macOS:   ~/Library/Application Support/blinken/blinken.db
Linux:   $XDG_DATA_HOME/blinken/blinken.db
         or ~/.local/share/blinken/blinken.db
Windows: %LOCALAPPDATA%\blinken\blinken.db
```

Allow explicit override through:

- `BLINKEN_DB`
- a global `--db <path>` option

Requirements:

- Enable write-ahead logging (WAL).
- Set a bounded busy timeout and keep write transactions short.
- Use automatic, ordered, versioned schema migrations.
- Back up or otherwise make migration failure recoverable before destructive migrations are introduced.
- Use foreign keys.
- Avoid exposing raw SQL or schema details as the public automation contract.
- Make database inspection with ordinary SQLite tools possible and documented as an escape hatch.

The likely initial tables are:

- `projects`
- `project_paths`
- `sessions`
- `guesses`
- `guess_files`

Later similarity support may add:

- `subjects`
- `embeddings`
- optional dismissed-similarity pairs

Do not create similarity tables until that capability is implemented unless reserving a nullable `subject_id` materially simplifies the first migration.

## 14. Semantic similarity and grouping

Semantic similarity is an optional capability, likely after v0.1. It must not be required to record, list, or review guesses.

### 14.1 Requirements

- No LLM query is required to record or match a guess.
- No hosted embedding API, account, token spend, or network call is part of the feature.
- Use a small local embedding model suitable for short software-decision text.
- Keep the embedding generator behind a thin boundary because runtime, model, packaging, and Go integration constraints may change the implementation.
- If embedding generation fails or is unavailable, recording still succeeds.

Conceptual boundary:

```go
type EmbeddingGenerator interface {
	Generate(ctx context.Context, text string) (Embedding, error)
}
```

### 14.2 Embedding input

Build normalized input from semantically meaningful fields:

```text
summary
ambiguity
chosen behavior
reason
alternative
would-have-asked
```

Do not embed timestamps, model names, absolute paths, or session identifiers. Cwd and file locations are structural signals and should be considered separately if later evidence shows they improve ranking.

### 14.3 Storage

Store vectors as compact binary data in SQLite along with:

- embedding model identifier
- model version or checksum
- dimensions
- numeric encoding
- embedding-input format version
- generation timestamp

Vectors produced by different model versions must not be compared as though they share a space. A later command such as `blinken embeddings rebuild` may regenerate vectors.

### 14.4 Matching

Start with brute-force cosine similarity within the current project and compatible embedding version. This is adequate for the expected first thousands of short records and is substantially simpler than an approximate-nearest-neighbor index.

Do not add a vector database or ANN index until measured data demonstrates a performance problem.

The initial similarity result is a suggestion:

```text
Possibly related:
0.91  Suspended accounts retain file-read access
0.86  Account suspension blocks uploads but not downloads
```

Similarity thresholds must be evaluated against real Blinken records rather than generic semantic-search benchmarks.

### 14.5 Human-confirmed subjects

The user may group suggested guesses. That explicit action:

- creates or selects a durable subject
- assigns `subject_id` to the selected guesses
- allows the subject to receive a stable human-readable name or key

Blinken must never silently create canonical subjects from similarity alone. A `not related` action should suppress the same unhelpful suggestion if that can be implemented simply.

Once subjects exist, later versions may compare new guesses with representative vectors or a subject centroid and surface recurring ambiguities across sessions.

### 14.6 Structural locality

Project, project-relative cwd, and explicit file references may later adjust relatedness. For example, two semantically similar guesses in the same component may be more relevant than the same score across unrelated parts of a monorepo.

Store the underlying context now, but do not invent a combined semantic/structural scoring formula in v0.1. Any later weighting must be backed by evaluation data and remain explainable.

## 15. Browser review experience

Implement `blinken review --serve` in the Go executable using the standard `net/http` server. Package the browser UI as static HTML, CSS, and JavaScript in a Go package and compile it into the executable with `//go:embed` and `embed.FS`.

[Crit](https://github.com/tomasz-tomczyk/crit) is useful architectural prior art for this shape: its Go process serves embedded frontend assets and local HTTP endpoints from one binary. Blinken should adopt that packaging pattern, not Crit's broader review, proxy, or daemon feature set.

Prefer plain HTML, CSS, and small vanilla JavaScript modules for v1 so the frontend needs no runtime dependency or separate installation. A frontend build step may be added later if the interface becomes complex enough to justify it; any generated assets must still be embedded into the released executable. The browser should use same-origin JSON or form endpoints exposed by the local Go server.

The initial browser UI should provide:

- Current project and optional session selection.
- Counts for unreviewed, high-impact, and low-confidence guesses.
- A priority-sorted review queue.
- Filters for status, kind, confidence, impact, and session.
- Full guess details, including ambiguity, choice, reason, alternative, and would-have-asked text.
- Project-relative cwd and file references where available.
- Accept, reject, and follow-up actions.
- Checkbox or current-filter bulk actions for accept, reject, and follow-up.
- An optional review note.
- Clear movement to the next unresolved guess after an action.

Keyboard shortcuts are desirable but not a release blocker:

```text
j / k  next / previous
a      accept
r      reject
f      follow-up
enter  expand or focus
```

Similarity suggestions and subject grouping belong to the similarity phase, not the base browser MVP.

## 16. Local web security

The review server has write access to the Blinken database and must be treated accordingly.

For v1:

- Bind only to `127.0.0.1` and `::1`, not all interfaces.
- Do not provide an external-listen option unless authentication and threat behavior have been deliberately designed.
- Use a random available port by default.
- Reject non-loopback `Host` headers while serving on loopback to reduce DNS-rebinding risk.
- Apply standard anti-CSRF protection to every state-changing request.
- Do not enable permissive CORS.
- Do not render user-provided content as raw HTML.
- Make the terminal output state the exact bound URL.
- Stop the server when the foreground process exits.

Loopback binding reduces exposure but is not a substitute for safe request handling.

## 17. Privacy, redaction, and deletion

Blinken may contain proprietary decisions, paths, file names, URLs, or secrets accidentally supplied in free text. The database should be documented as sensitive local data.

The program must:

- Never capture environment variables automatically.
- Never capture full command output automatically.
- Never crawl or persist source-file contents as context.
- Capture only explicitly supplied free text plus the small set of inferred metadata documented here.
- Apply configured redaction before persistence, not only at display time.
- Allow cwd and file capture to be disabled.
- Soft-delete individual guesses and filtered sets by recording `deleted_at`.
- Exclude soft-deleted records from normal queries and review.
- Avoid telemetry. There is no telemetry in v1.

Potential commands:

```bash
blinken delete <guess-id>
blinken delete --project . --before 2026-01-01
blinken purge --hard --deleted-before 2026-01-01
```

`delete` is soft deletion in v0.1. A restore UI and ordinary restore command are not required yet, but the retained row must be structurally recoverable in a later version. `purge --hard` is the deliberately explicit privacy escape hatch for permanent removal of already soft-deleted records. Both filtered deletion and hard purge show the number of matching records and require confirmation unless `--yes` is supplied.

Whether SQLite space is reclaimed immediately after hard purge is an implementation detail that should be documented honestly.

Redaction may include literal values and user-configured regular expressions or glob-like path rules. Built-in heuristics may catch common credential shapes, but Blinken must not claim that heuristic secret detection is complete.

Database encryption is not an MVP feature. Users who need encryption at rest should rely on operating-system disk encryption and appropriate filesystem permissions unless a concrete stronger requirement emerges.

## 18. Configuration

Configuration is optional. Defaults should work without a config file.

Use OS-appropriate user configuration storage. A project-local configuration file may be supported later for recording guidance and privacy rules, but Blinken should not write into repositories merely because it was invoked there.

Potential settings:

```toml
[recording]
capture_cwd = true
capture_files = true

[privacy]
redact = ["literal-sensitive-value"]
ignore_paths = [".env", "secrets/**"]

[review]
open_browser = true
include_followup = true
```

Command-line options override environment variables, which override user configuration, which overrides built-in defaults. Project configuration, if added, needs an explicit precedence rule during grilling.

## 19. Implementation architecture

Use a supported stable Go release for v1.

Suggested components:

- CLI command parsing and output formatting.
- Core models, validation, ranking, and use cases.
- SQLite persistence and migrations.
- Local review server and embedded browser UI.
- Optional similarity component added later.

Start with the fewest packages that keep the code understandable. A reasonable first shape is:

```text
cmd/
  blinken/          # package main and composition root
internal/
  cli/              # command parsing and output contracts
  core/             # models, validation, ranking, and use cases
  storage/          # SQLite access and migrations
  server/           # loopback HTTP handlers
web/                # embedded browser assets and embed.go
```

The `cmd/blinken` package should remain a thin composition root. Keep packages cohesive, but do not create interfaces and package boundaries merely to imitate a large service architecture. Six layers for a tiny first release would be theater.

Implementation choices:

- Standard-library `net/http` for local HTTP endpoints, with explicit server timeouts and graceful shutdown when the foreground command exits.
- `embed.FS` for frontend assets, served from the same process and origin as the review API.
- Plain HTML, CSS, and vanilla JavaScript unless measured UI complexity justifies a frontend toolchain.
- `database/sql` with small explicit data-access code and a deliberately selected SQLite driver.
- A mainstream Go CLI parser chosen after checking maintenance, help output, testability, and parsing ergonomics; the standard `flag` package remains acceptable if it satisfies the command shape.

The intended artifact is one executable containing the CLI, local server, migrations, and browser assets. SQLite driver selection may affect CGO use and cross-compilation, so prove the build matrix early. Do not distort the domain model, web UI, or migration strategy merely to avoid CGO before the tradeoff has been measured.

## 20. Deferred packaging and distribution

The primary installation experience should not require npm or Python.

Automated release production, GitHub Actions release workflows, checksummed platform artifacts, and package-manager publication are explicitly not v0.1 objectives. Initial development may use `go run ./cmd/blinken`, `go build`, and local scripts.

After the core workflow is proven, evaluate these channels:

- GitHub Releases with checksummed self-contained binaries for supported operating systems and architectures.
- Homebrew for macOS and Linux users.
- winget and/or Scoop for Windows.
- `go install <module>/cmd/blinken@<version>` for users who already have a suitable Go toolchain.

The exact channel order should follow real maintainer capacity. GitHub Releases are a likely future source of truth; package-manager manifests can point to those artifacts.

Future release expectations:

- Reproducible scripted builds.
- Version embedded in `blinken --version`.
- SHA-256 checksums.
- A software bill of materials if it can be generated with low maintenance cost.
- Clear supported-platform matrix.
- Smoke testing of packaged artifacts, not only local development builds.

Pure-Go and CGO-backed SQLite builds should be compared empirically for correctness, portability, build complexity, performance, and artifact size. Package size alone should not decide the architecture.

## 21. Testing expectations

The implementation should include:

### Unit tests

- Guess validation and defaults.
- Review-priority ordering and tie-breaking.
- Project-relative path handling.
- Compact text formatting and JSON schema.
- Redaction before persistence.
- Similarity input normalization when that phase is implemented.

### Storage integration tests

- Fresh database creation.
- Ordered migration from each supported schema version.
- WAL mode and concurrent short writes from multiple connections.
- Busy-timeout behavior.
- Foreign-key enforcement.
- Review status updates.
- Deletion and purge scoping.
- Recovery behavior for failed migrations where applicable.

Use temporary local databases and deterministic fixtures. Tests must not require network access or cloud credentials.

### CLI end-to-end tests

- Minimal `guess` invocation prints only an ID and creates the expected record.
- `--quiet`, redirected output, `NO_COLOR`, stderr, and exit-code contracts.
- Project and cwd detection in Git, Jujutsu, and non-repository fixtures.
- Session environment-variable and explicit-option precedence.
- Compact output remains stable enough for agents and wrappers.

### Browser tests

- Server binds only to loopback.
- Every required frontend asset is present in the compiled `embed.FS` and served with the expected content type.
- Review queue and filters render safely.
- Accept, reject, follow-up, and note submission update the database.
- User text is HTML-escaped.
- CSRF protection rejects invalid state-changing requests.

### Deferred cross-platform and packaging tests

Cross-platform CI and smoke tests against packaged artifacts should be added when automated releases enter scope. They do not block v0.1, but the chosen SQLite driver must be proven on each initially supported platform before release.

### Similarity evaluation

Before enabling suggestions by default, build a small labeled set of real or representative guesses. Measure whether the selected local model retrieves meaningfully related decisions and how often it produces misleading near-matches. The acceptance threshold is a product decision, not merely a cosine number.

## 22. MVP acceptance criteria

Version 0.1 is useful when:

1. An agent can start a session and record a guess with one short command.
2. Recording succeeds offline into the user-level SQLite database.
3. Multiple local sessions can write without ordinary lock failures.
4. Project, cwd, time, and supplied metadata are persisted accurately.
5. The default successful write output is only an identifier.
6. A user can list and review unresolved guesses in deterministic priority order.
7. A user can accept, reject, or mark a guess for follow-up with an optional note.
8. `blinken review --serve` provides the same core review actions in a safe loopback-only browser UI.
9. No telemetry, network request, LLM call, or hosted account is required.
10. Tests cover the ranking, storage, CLI contract, redaction boundary, and browser write actions.

Embeddings, similarity suggestions, and subjects are not required for v0.1 acceptance.

## 23. Phased roadmap

### Phase 0: Product spike

- Validate the proposed CLI vocabulary with hand-written example sessions.
- Decide the CLI parser and ID format.
- Prove SQLite WAL behavior with concurrent writers.
- Prove a single Go executable can embed the browser assets and conditionally host the loopback review UI.

### Phase 1: v0.1 useful ledger

- User-level SQLite database and migrations.
- Project and cwd detection.
- Explicit and optional sessions.
- `guess`, `guesses`, `show`, review-state commands, and terminal `review`.
- Deterministic review priority.
- Compact output and optional JSON.
- Loopback-only `review --serve` with a basic embedded browser UI.
- Privacy controls, redaction boundary, deletion, and no telemetry.
- Reference agent skill or prompt explaining when to record a guess.

### Phase 2: Similarity experiment

- Select and package a lightweight local embedding model.
- Lazy or post-insert embedding generation.
- Versioned vector storage.
- Brute-force project-scoped cosine matching.
- Browser and CLI suggestions for possibly related guesses.
- Labeled evaluation set and threshold tuning.

### Phase 3: Human-confirmed recurring subjects

- Explicit grouping and ungrouping.
- Stable subject identifiers created after grouping.
- Dismissed-match handling.
- Cross-session recurring-guess summaries.
- Experimental structural-locality adjustments using cwd and files.

### Later, only if justified

- Automated release workflows and checksummed platform artifacts.
- Homebrew, winget, Scoop, and `go install` publication.
- Additional platform and architecture builds where demand justifies them.
- Import/export and backup ergonomics.
- Better project relocation and alias management.
- Integrations with `frick`.
- Editor or agent-protocol integrations.
- Richer interactive UI, potentially including a frontend build tool or framework if vanilla JavaScript becomes a demonstrated constraint.

## 24. Decisions and open questions

### 24.1 Resolved owner decisions

1. **Open-source license:** MIT.
2. **Omitted ranking metadata:** Default confidence, impact, and reversibility to `medium`, `medium`, and `moderate` so the one-argument `guess` command remains valid.
3. **Guesses without an explicit session:** Leave `session_id` null. Do not invent implicit sessions in v0.1.
4. **Default review scope:** Include both `unreviewed` and `followup` guesses, with unreviewed guesses first when priority is otherwise equal. Make inclusion of follow-up items configurable.
5. **Review state and bulk actions:** Persist the current status, note, and timestamp across review sessions. Provide bulk actions so the user can resolve low-value items together or leave uncertain items for later. Do not add a historical event log in v0.1.
6. **Deletion semantics:** Soft-delete by default. A v0.1 restore UI is unnecessary, but data should remain recoverable while early agent behavior and usage patterns are being learned. Keep permanent purge as a separate explicit operation.
7. **Release scope:** Defer automated releases, GitHub Actions release workflows, platform artifacts, and package-manager publication until after v0.1.

### 24.2 Engineering questions for interrogation

These should be resolved through focused research, prototypes, or measured use. The implementation agent should bring back evidence and a recommendation rather than asking the owner to choose blindly.

1. Which ID format best balances time ordering, Go ecosystem support, and readability: UUIDv7 or ULID?
2. Which Go CLI parser has the best combination of maintenance, help UX, testability, and parsing ergonomics, or is the standard `flag` package sufficient?
3. What is the least surprising project identity and relocation workflow for Git worktrees, Jujutsu, monorepos, and directories with no VCS?
4. Does v0.1 need stdin or structured JSON input, or is option-based recording enough?
5. Which path fields should be displayed as absolute versus project-relative, and what conservative redaction defaults are appropriate?
6. Are project configuration files useful enough to justify their precedence and privacy implications in v0.1?
7. Which local embedding model and runtime provide acceptable semantic quality, package size, cross-platform behavior, license, and Go integration?
8. Should embeddings be generated after insert, lazily during review, or only by an explicit maintenance command?
9. What labeled examples and success criteria are sufficient to enable similarity suggestions by default?
10. Which Go SQLite driver gives Blinken the best balance of SQLite correctness, WAL behavior, cross-platform single-binary builds, CGO requirements, maintenance, and release complexity?

## 25. Reference guidance for coding-agent skills

A companion skill should tell an agent:

> Record a guess when you must choose among materially different outcomes and the specification, tests, documentation, or established project conventions do not clearly determine the answer.

Worth recording:

- unspecified error, retry, fallback, or persistence behavior
- destructive or data-retention semantics
- security and authorization assumptions
- compatibility and externally visible API decisions
- inferred business rules
- ambiguous acceptance criteria
- deviations from a request
- architecture choices where requirements do not distinguish meaningful alternatives

Usually not worth recording:

- local variable names
- formatting
- mechanical refactors
- obvious language idioms
- choices fully determined by established project conventions

An ideal guess says:

1. What was ambiguous.
2. What choice was made.
3. Why that choice was made.
4. What alternative was plausible.
5. What the agent would have asked if asking had no cost.

The skill should favor recording at the moment of the decision, not reconstructing a ledger at the end of the run.

## 26. Final constraint

Blinken should earn complexity from observed use.

The first release needs to make one loop excellent:

```text
agent encounters consequential ambiguity
→ agent records one cheap guess
→ human sees the important guesses first
→ human accepts, rejects, or follows up
```

Everything else is optional until that loop proves useful.
