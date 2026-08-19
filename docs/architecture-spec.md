# Habitat — Architecture Spec

## Languages

| Component | Language | Why |
|---|---|---|
| Engine + CLI + Server | Go | One static binary, no runtime to install on a dev machine or CI box; good HTTP + SQLite story; fast enough to grade and persist without adding noticeable latency to a run. |
| First-party SDK | Ruby | Matches the first real target this is being built against. |
| SQLite | — | Single-file store; no separate database process to run for a single-team, self-hosted tool. `habitat serve` can later grow a Postgres backend without changing the wire protocol, but nothing in v1 needs that. |

There is deliberately **one** implementation of the suite format and the
grading/policy logic, in Go. Every SDK, in every language, is a thin client
against a versioned HTTP wire protocol — it registers targets and executes
them, nothing more. This is the central design decision: it means adding a
second language later is "implement a small HTTP client," not "port a
grading engine and hope it stays in sync."

## Components

```
                          ┌──────────────────────────────────────┐
                          │              habitat (Go)             │
                          │                                        │
  habitat run suite.yml   │  ┌──────────┐   ┌─────────┐   ┌──────┐ │
  ───────────────────────▶│  │  suite   │──▶│ ingest   │──▶│grade │ │
                          │  │  parser  │   │ server   │   │+policy│ │
                          │  └──────────┘   └────▲─────┘   └───┬──┘ │
                          │                       │             │   │
                          │                  HTTP │             ▼   │
                          │                       │        ┌────────┐│
                          │                 ┌─────┴────┐   │ SQLite ││
                          │                 │  runner   │   │ store  ││
                          │                 │ subprocess│   └────────┘│
                          │                 │ (Ruby SDK)│             │
                          │                 └───────────┘             │
                          └──────────────────────────────────────────┘

  habitat serve            ┌──────────────────────────────────────┐
  ───────────────────────▶ │  web dashboard + ingest API over the  │
                            │  same SQLite store, long-running      │
                            └──────────────────────────────────────┘
```

### `habitat run <suite.yml> [-- <runner-command>]`

1. The Go process parses and validates the suite file. This is the only
   place the suite format is read.
2. It computes the full list of executions to perform — every `case_id` ×
   its repetition count — and starts a local HTTP **ingest server** on an
   ephemeral `127.0.0.1` port.
3. It launches `<runner-command>` (e.g. `bundle exec ruby run.rb`) as a
   subprocess, with `HABITAT_URL` and `HABITAT_RUN_TOKEN` set in its
   environment.
4. The runner subprocess (built with a language SDK) `GET`s the execution
   list from the ingest server, calls its locally registered target for
   each one, and `POST`s each `Habitat::Result` back as it completes. The
   SDK never sees suite YAML, grader config, or policy — only
   `{case_id, repetition_index, input}` in and `Result` out.
5. When the subprocess exits (or signals completion), the Go process grades
   every received execution against its case's `expectations`, evaluates
   the suite's `policy`, persists the finished run to SQLite, prints the
   report, and exits with the corresponding exit code.
6. With a server configured (`server:` in habitat.yml, or `--server=URL`),
   the finished, already-graded run is also reported to it for shared
   visibility. Local grading never depends on a remote server being up, and a
   failed push never changes the exit code.

This is why "runner" and "engine" are one process: the Go binary that
parses the suite, receives the pushed results, grades them, and persists
them is the same binary the user typed at the command line. There's no
separate long-lived "runner engine" service to deploy for `run` to work.

### `habitat serve`

The same binary, in long-running mode: an HTTP server over a SQLite file,
serving the web dashboard and the HTML report for a single run, and exposing
the ingest API (`POST /v1/runs`) that `habitat run --server=…` pushes to.

It is **multi-tenant**. A project owns its runs and holds the token a CLI
authenticates with; people sign in with an account (bcrypt password, session
cookie) to read reports. Local runs land in an implicit `local` project whose
token hash is unmatchable, so nothing can write to them over the network.

Two invariants: the server **refuses to start unauthenticated off loopback**
(a flag can be set wrongly; refusing cannot), and runs are **scoped to their
project**, so guessing a run id from another tenant yields a 404. Deployed
with Kamal; SQLite sits on a volume so a deploy never discards history.

There is no separate "server" codebase — it reuses the `internal/store`,
`internal/report` and `internal/policy` packages `run` already uses; the delta
is an HTTP handler layer and templates instead of a terminal renderer.

## Wire protocol

JSON over HTTP, versioned under `/v1`. Defined once in Go
(`pkg/protocol`) so a future non-Go SDK has a single spec to implement
against (initially: read the Go structs and their JSON tags; a formal
OpenAPI doc can follow once the shape stabilizes).

| Endpoint | Direction | Purpose |
|---|---|---|
| `GET /v1/executions` | SDK ← engine | The list of `{case_id, repetition_index, input}` this runner should execute. |
| `POST /v1/executions/{case_id}/{repetition_index}` | SDK → engine | One execution's `Result`: `output`, `final_state`, `events`, `usage`, `duration_ms`, `error`. |
| `POST /v1/complete` | SDK → engine | Signals the runner is done; optional, since the engine also detects subprocess exit. |
| `POST /v1/runs` | remote push | A fully graded run, authenticated by a project token. The server stores the verdict and never re-grades. |
| `GET /v1/suites`, `/v1/runs`, `/v1/runs/{id}` | dashboard ← store | Read APIs backing the web UI. |

`Habitat::Result` (the only shape an SDK ever produces):

```json
{
  "output": {},
  "final_state": {},
  "events": [],
  "usage": { "cost_usd": 0.01 },
  "duration_ms": 812,
  "error": null
}
```

## Data model (SQLite)

```
suites        (id, name, target, source_path, digest, created_at)
runs          (id, suite_id, git_sha, started_at, finished_at,
               pass_rate, status, policy_json)
cases         (id, suite_id, case_id, title, critical, tags_json)
executions    (id, run_id, case_id, repetition_index, output_json,
               final_state_json, events_json, usage_json,
               duration_ms, error_json)
grades        (id, execution_id, grader_type, passed, detail_json)
```

`digest` on `suites` is a hash of the suite's grading-relevant fields
(everything except free-text `title`/`description`) — the same purpose as
excluding prose from a baseline comparison: editing a description never
invalidates history.

## Repo layout

```
habitat/
  cmd/
    habitat/                # main package — CLI entrypoint, subcommand dispatch
  internal/
    suite/                  # YAML schema + parsing + validation (the one parser)
    engine/                 # execution-list computation, run orchestration for `run`
    ingest/                 # local HTTP server `run` starts; also mounted by `serve`
    graders/                # no_error, exact_match, includes, json_schema, state_match, rubric, ...
    policy/                 # suite policy evaluation
    baseline/               # run-vs-run diffing (Phase 2)
    store/                  # SQLite schema, migrations, queries
    report/                 # terminal / json / junit / html rendering
    server/                 # habitat serve: dashboard HTTP handlers
      web/                  # templates + static assets, embedded via embed.FS
  pkg/
    protocol/               # versioned wire types shared by engine and (future) SDKs
  sdk/
    ruby/                   # habitat-sdk gem
      lib/habitat/
        target.rb           # target registry
        result.rb           # Habitat::Result
        runner.rb           # fetch → call → post loop
        client.rb           # thin HTTP client
      habitat-sdk.gemspec
      spec/
  examples/
    suites/                 # example suite YAML, used in docs and for dogfooding
  docs/
    product-spec.md
    features-spec.md
    architecture-spec.md
  go.mod
  CLAUDE.md
```

## Extension points

| Extension | Registered how | Built-in |
|---|---|---|
| Graders | `internal/graders` registry, keyed by `type:` | All MVP graders ship pre-registered. |
| Judge (backs `rubric`) | Configured on the engine; must implement `Score(criteria, input, output) -> JudgeResult` | None — a suite using `rubric` with no judge configured fails that one expectation cleanly. Phase 2. |
| Target registry | SDK-side, per language (e.g. Ruby's `Habitat::Target.register(:key, klass)`) | None — every target is app-specific. |

## Declared limits

Mirrors the shape of any eval engine that has to guard against an
accidental expensive run — enforced by the engine before it starts pushing
executions to a runner.

| Setting | Default | Meaning |
|---|---|---|
| `default_repetitions` | 1 | Fallback when neither suite nor case sets one. |
| `maximum_repetitions` | 10 | Ceiling on repetitions per case. |
| `maximum_cases_per_run` | 200 | Ceiling on cases in a single suite run. |

## Why this shape

- **One suite-format parser, one grader implementation, one policy
  evaluator** — all in Go, all exercised identically regardless of which
  language's SDK produced the raw execution. A second-language SDK is a
  small, low-risk addition rather than a second place the format can drift.
- **The SDK is deliberately dumb.** It registers targets and speaks one
  small HTTP contract. This keeps the barrier to a new language low and
  means a bug in grading logic only ever needs fixing once.
- **The CLI *is* the runner engine.** `habitat run` doesn't shell out to
  a separate "engine" process — it *is* the process that parses the suite,
  receives the pushed results, grades them, and persists them. `habitat
  serve` is the same binary, long-running, fronting the same store.
- **Persistence is not optional.** Unlike a harness that only prints a
  report unless you configure a database, a shared, navigable run history
  is the entire point of having a server component — every run lands in
  SQLite by default.
