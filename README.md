# habitat

An evaluation engine for code whose output can't be asserted byte-for-byte —
LLM agents, prompts, ranking pipelines, anything where the same input can
legitimately come back two different ways and still be right.

It runs hand-verified cases against your real application code, grades each
execution, and keeps a browsable history of every run.

```
  PASS  a question is answered, not acted on   3/3 repetitions passed
  FAIL  asking for an exercise proposes it     1/3 repetitions passed
          state_match: proposed: expected true, got false

  2 cases, 6 executions, 67% pass rate
  FAILED  (run 8cfe0504a9551b59)
```

## Why

A deterministic test asserts exact output. Code that calls a real model can't be
graded that way, and mocking the model away only tests the plumbing around it —
a fake client asserts *your orchestration*, not the model's judgement, and can't
reproduce what the real API does.

habitat fills that gap, and is deliberately **not** part of CI: every execution
costs real money, so it's run on purpose.

## How it fits together

One Go binary is both the CLI and the engine. `habitat run` parses the suite,
starts a local ingest server, launches your runner as a subprocess, grades what
it reports back, applies the suite's policy, and persists the run.

Your runner is a process in **your** language, built with a thin SDK that only
registers agents and hands off. It never parses a suite, never grades, never
asserts — so adding a language means writing a small HTTP client, not a second
copy of the engine.

```
habitat run  ─▶  suite ─▶ ingest server ◀── HTTP ── runner (your language)
                            │                         └─ calls your real code
                            ▼
                     grade ─▶ policy ─▶ SQLite ─▶ report / dashboard
```

Nothing in habitat knows what a model provider is. Cost is whatever your target
reports; when nothing reports it, reports omit cost rather than printing a
misleading `$0.00`.

## Install

```bash
./install.sh          # the habitat binary + the Claude Code skill
```

Or just the binary:

```bash
go install github.com/gronnbeck/habitat/cmd/habitat@latest
```

## Try it

`examples/` is a complete working project with a fake agent that costs nothing:

```bash
habitat validate --dir examples     # free — calls no target
habitat run echo --dir examples     # runs the whole loop
habitat serve --dir examples        # browse runs at :7878
```

## A suite

```yaml
name: coach chat
target: coach_chat
default_repetitions: 1

policy:
  minimum_pass_rate: 1.0
  critical_cases_must_pass: true

cases:
  - id: a question is answered, not acted on
    critical: true
    input:
      message: How many sets am I doing?
    expectations:
      - type: no_error
      - type: state_match
        path: proposed
        value: false
```

Grade the structured fields an agent returns, not its prose — wording varies
between correct runs, so asserting on a sentence flakes on phrasing rather than
catching a regression.

## A runner

```ruby
require "habitat"

Habitat.target "coach_chat" do |input:, context:|
  reply = CoachChatResponder.call(...)

  Habitat::Result.new(
    output: reply[:text],
    final_state: { proposed: !reply[:proposal].nil? }
  )
end

Habitat.start
```

```ruby
gem "habitat-sdk", github: "gronnbeck/habitat", glob: "sdk/ruby/*.gemspec"
```

## Reporting to a server

`habitat serve` is the same binary long-running, and it is multi-tenant. A
**project** owns its runs and holds the token a CLI authenticates with; people
sign in with an account to read the reports.

```yaml
# habitat.yml — safe to commit; the token never lives here
server: https://habitat.example.com
```

```bash
export HABITAT_TOKEN=hbt_…
habitat run coach_chat
```

Or keep the token in `.habitat.env` beside `habitat.yml` — habitat loads it
automatically. Gitignore it; it holds a credential. Anything already set in
the environment takes precedence, so overriding for one run still works.

```
  PASSED  (run ad054b99b29cd540)

  reported to https://habitat.example.com/projects/chikara/runs/ad054b99b29cd540
```

The run still prints to your terminal and still records locally. Grading
happens locally and the server stores the verdict — it never re-grades — so a
run reads the same in both places, and **a failed push never changes the exit
code**: an unreachable server is not a failing evaluation.

The first account is created through `/signup` using the server's
`HABITAT_SIGNUP_TOKEN` — which is also how you invite colleagues. Unset the
token to close signups; existing accounts keep working.

```bash
habitat admin create-project "Chikara"    # prints the project token once
habitat admin create-user ken@example.com # or create an account directly
```

Two rules the server enforces: it **refuses to start unauthenticated off
loopback**, rather than trusting a flag not to publish your prompts and model
output; and runs are **scoped to their project**, so a run id from elsewhere is
a 404 rather than a readable report. On loopback it runs open, so local use
needs no account.

It deploys with Kamal — see `config/deploy.yml` and the `Dockerfile` (a 35 MB
image; Ruby appears in this repo only to pin Kamal's version).

## Cost

`validate`, `list`, `show` and `serve` are free — they never call a target.
`run` is not: one real provider call per execution, multiplied by repetitions.
Validate first, and confirm the spend before running.

## Documentation

- [`skills/habitat/SKILL.md`](skills/habitat/SKILL.md) — the full CLI guide:
  graders, policy, the SDK contract, troubleshooting
- [`docs/product-spec.md`](docs/product-spec.md) — what it is and why
- [`docs/features-spec.md`](docs/features-spec.md) — the feature set and phasing
- [`docs/architecture-spec.md`](docs/architecture-spec.md) — how it's built

## Status

The engine works end to end: suite parsing and validation, the MVP graders,
policy, SQLite persistence, terminal and JSON reports, and the `serve`
dashboard. Ruby is the first SDK.

Not built yet: baseline diffing, the `rubric` grader and its judge, trace-based
graders, JUnit/HTML reports, and generated INV/DIR case variants.

## Licence

MIT
