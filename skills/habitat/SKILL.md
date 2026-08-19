---
name: habitat
description: How to use the `habitat` CLI — an evaluation engine that grades non-deterministic code (LLM agents, prompts, ranking pipelines) against hand-verified cases, in any language, via a language SDK that reports results over HTTP. Covers writing suites, the grader and policy reference, the Ruby SDK, reading a run, and the cost discipline (`validate` is free, `run` bills a real provider). Use when asked to evaluate/eval an agent or prompt, check whether a prompt or model change regressed something, write or fix a habitat suite, interpret a habitat run, set habitat up in a project, or when a change touches LLM-calling code that ordinary tests can't grade.
---

# habitat

An evaluation engine for code whose output cannot be asserted byte-for-byte. It
runs hand-verified cases against real application code, grades each execution,
and keeps a browsable run history.

Source: https://github.com/gronnbeck/habitat · binary: `habitat` · Go engine, per-language SDKs.

## When to use it — and when not to

**Use it** when correctness is a judgement rather than an equality:

- an LLM agent that extracts, classifies, routes, or answers
- a prompt or model change, to see whether it regressed a verified behaviour
- comparing two implementations of the same agent
- any pipeline where the same input can legitimately produce two different correct outputs

**Don't use it** for deterministic code. If `assert_equal` works, use the normal
test suite — it's free, fast, and runs in CI. habitat is deliberately *not* part
of CI: every execution hits a real provider and costs real money.

The gap it fills: a unit test with a faked model client asserts *your
orchestration*, not the model's judgement — and a fake can't reproduce what the
real API does. That blind spot is exactly where habitat earns its keep.

## Cost discipline — read before running anything

| Command | Costs money? |
|---|---|
| `habitat validate`, `list`, `show`, `serve` | **No** — never calls a target |
| `habitat run` | **Yes** — one real provider call per execution, × repetitions |

**Always `habitat validate` first** — it catches every structural mistake for
free. **Confirm with the person who owns the budget before `habitat run`**, the
same way you would for any other cost-incurring action. Say how many executions
it will be: `cases × repetitions`.

Nothing about habitat is provider-specific. It never computes a cost — cost
appears in reports only if the target reports it.

## Commands

```bash
habitat validate [suite...]     # validate one or all suites — free, run this first
habitat list                    # suites found, with target and case count
habitat run <suite>             # run it; costs money
habitat show <run-id>           # print a persisted run
habitat serve                   # browse run history at :7878
```

### `run` flags

| Flag | Effect |
|---|---|
| `-- <command>` | Runner command, if not set in `habitat.yml` (e.g. `-- bin/rails runner evals/runner.rb`) |
| `--repetitions=N` | Override the suite's repetition count |
| `--format=terminal\|json` | `json` is the full payload |
| `--output=PATH` | Write the report to a file |
| `--dir=PATH` | Project directory holding `habitat.yml` (default `.`) |
| `--db=PATH` | SQLite file (default `.habitat/habitat.db`) |
| `--no-persist` | Don't record this run |
| `--timeout=30m` | Whole-run wall clock |

Other commands: `validate` and `list` take `--dir` only; `show` adds `--db` and
`--json`; `serve` adds `--db` and `--addr` (default `127.0.0.1:7878`).

### Exit codes

`0` every case passed · `1` a case failed · `2` invalid suite or config, nothing ran · `3` framework error, or the run broke

`2` and `3` mean *don't trust the numbers* — nothing was evaluated, or not all of it.

## Project layout

```
habitat.yml                 # suites dir, runner command, db path
evals/
  suites/<name>.yml         # one suite per file
  runner.rb                 # declares targets, then hands off
.habitat/habitat.db         # run history (gitignore this)
```

```yaml
# habitat.yml
suites: evals/suites
runner: ["bin/rails", "runner", "evals/runner.rb"]
db: .habitat/habitat.db
```

## Suite format

```yaml
name: coach chat
target: coach_chat            # key the SDK registered
default_repetitions: 1        # each repetition is a real call — raise deliberately

policy:
  minimum_pass_rate: 1.0
  critical_cases_must_pass: true

cases:
  - id: a question is answered, not acted on
    title: Prose only — excluded from the digest, so editing it never invalidates history
    description: >
      Why this case exists and how its expected answer was verified. This is
      where the source material and the hand-verification method belong.
    critical: true            # fails the suite outright under critical_cases_must_pass
    tags: [coach, no-tool]
    repetitions: 3            # overrides the suite default for this case
    input:                    # passed to the target opaquely
      message: How many sets am I doing?
    expectations:
      - type: no_error
      - type: state_match
        path: proposed
        value: false
```

Rules worth knowing:

- A case with **no expectations is rejected** — it would pass forever.
- Case ids must be unique; they're the identity used across runs.
- `title`/`description` are **excluded from the suite digest**, so improving the
  prose never breaks comparability with earlier runs.
- Repetitions are capped at 10, cases at 200.
- A misspelled field, grader type, grader option, or schema keyword is a
  **load-time error**, not a silent no-op. A check the author believes they have
  is the failure mode this guards against.

## Graders

Each entry in `expectations` names one `type`; the other keys are its options.

| Type | Passes when | Options |
|---|---|---|
| `no_error` | The target didn't error | — |
| `state_match` | Value at `path` in `final_state` equals `value` | `path`, `value` |
| `exact_match` | Value equals `value` exactly | `value`, `path`, `source` |
| `includes` | Substring (strings) or membership (arrays) | `value`, `path`, `source` |
| `json_schema` | Validates against a JSON Schema subset | `schema`, `path`, `source` |
| `maximum_duration` | Took at most `maximum_ms` | `maximum_ms` |
| `maximum_cost` | Reported cost at most `maximum_usd`; passes if no cost reported | `maximum_usd` |

**Grade structured fields, not prose.** Wording varies between correct runs, so
`includes`/`exact_match` on a sentence flakes on phrasing rather than catching a
regression. Reach for `state_match` first.

### `source` — the one that trips people up

`state_match` reads `final_state`. The other three read `output` **by default**.
An agent whose answer is prose keeps its gradeable structure in `final_state`,
so point them at it explicitly:

```yaml
- type: json_schema
  source: final_state        # ← without this it reads output and finds nothing
  path: action_types
  schema:
    type: array
    contains: { type: string, const: add_exercise }
```

Seeing `no value at output path "x"` almost always means a missing `source: final_state`.

### JSON Schema subset

`type`, `required`, `properties`, `additionalProperties`, `items`, `contains`,
`not`, `enum`, `const`, `minimum`, `maximum`, `exclusiveMinimum`,
`exclusiveMaximum`, `minLength`, `maxLength`, `pattern`, `minItems`, `maxItems`,
`uniqueItems`, `nullable`. Anything else is a load-time error.

For "this specific thing is present" in an unordered list use `contains`, not
`items` (which demands *every* element match). Wrap it in `not` for absence:

```yaml
schema:
  type: array
  not:
    contains: { type: object, properties: { title: { type: string, pattern: "(?i)fraud" } } }
```

## Policy

Suite-level rules on top of individual cases. Unset, the default already
requires every case to pass every repetition, so a `policy:` block only ever
adds constraints.

| Key | Fails the suite when |
|---|---|
| `minimum_pass_rate` (default `1.0`) | Pass rate below this fraction |
| `critical_cases_must_pass` | Any `critical: true` case failed |
| `maximum_error_rate` | Share of errored executions exceeds this |
| `maximum_average_cost` | Mean reported cost per execution exceeds this (silent if no cost reported) |
| `maximum_average_duration_ms` | Mean duration exceeds this |

## The SDK contract

A runner **declares agents and hands off**. It never parses a suite, never
grades, never asserts. Everything after `start` — fetching the plan,
repetitions, timing, posting results, error isolation — is library code.

```ruby
# evals/runner.rb
require_relative "../config/environment" unless defined?(Rails.application)
require "habitat"

Habitat.target "coach_chat" do |input:, context:|
  reply = CoachChatResponder.call(...)

  Habitat::Result.new(
    output: reply[:text],                    # prose — content graders read this
    final_state: {                           # structured — state_match reads this
      proposed: !reply[:proposal].nil?,
      action_count: (reply[:proposal]&.actions || []).length
    }
  )
end

Habitat.start                                # hands off; blocks until done
```

Install (Ruby): `gem "habitat-sdk", github: "gronnbeck/habitat", glob: "sdk/ruby/*.gemspec"`

`Habitat::Result` fields — all optional: `output`, `final_state`, `events`,
`usage`, `duration_ms` (filled in automatically), `error`.

### Reporting cost (optional)

habitat never invents a cost. Report it yourself, or reports omit cost entirely
rather than printing a misleading `$0.00`:

```ruby
Habitat::Usage.new(cost_usd: 0.0123)                                  # you know the cost

Habitat.configure { |c| c.price "some-model", input: 5.0, output: 25.0 }   # USD per 1M tokens
Habitat::Usage.from_tokens(model: "some-model", input: 1200, output: 380)
```

Prices are yours to supply — habitat ships no price table and assumes no provider.

### Set up records without leaving rows behind

For a target that needs database state, build it in a transaction that always
rolls back. The agent may persist things (and even apply them); none of that
should survive an evaluation.

```ruby
ActiveRecord::Base.transaction do
  # ...build records, call the agent, capture the reply...
  raise ActiveRecord::Rollback
end
```

## Reading a run

```
  PASS  a question is answered, not acted on  3/3 repetitions passed
  FAIL  asking for an exercise proposes it    1/3 repetitions passed
          state_match: proposed: expected true, got false

  2 cases, 6 executions, 67% pass rate
  POLICY  critical case "asking for an exercise proposes it" failed
  FAILED  (run 8cfe0504a9551b59)
```

The `X/Y repetitions passed` fraction stays visible even when a case fails, so a
**consistently wrong** case (`0/3`) reads differently from a **flaky** one
(`2/3`). Treat them differently: the first is a bug, the second is a stability
problem — raise repetitions to measure it rather than re-running until it's green.

`habitat show <run-id> --json` gives the full payload including each execution's
`final_state`, `output`, and error — the fastest way to see what an agent
actually returned.

## Troubleshooting

**`INCOMPLETE — the runner reported no result for N of M executions`**
The runner crashed; the failures below it are evidence about the runner, not the
agent. Scroll up to its output. Usual causes: a syntax/boot error, wrong Ruby
version, missing dependency, or missing API key.

**`no value at output path "x"`** → you probably want `source: final_state`.

**`final_state has no value at "x"`** → the target isn't exposing that field;
fix the target, not the suite.

**`HABITAT_URL is not set`** → the runner was executed directly. It must be
launched *by* `habitat run`, which injects `HABITAT_URL` and `HABITAT_RUN_TOKEN`.

**Exit `2`** → a suite is invalid; the message names the case and option. Nothing ran.

**A target that raises** fails only its own case — the run continues, and the
exception message lands in that execution's error.

## Working on suites well

- **Validate free, then run.** `habitat validate` catches structural mistakes at
  no cost; only then consider spending.
- **Start at one repetition.** Raise it when you're specifically measuring
  stability, and remember the cost multiplies.
- **Never "fix" a case to make it green.** A failing case is a finding until
  proven otherwise. Change the expectation only when you can say why the old one
  was wrong.
- **Put the verification story in `description`.** It's excluded from the
  digest, so it costs nothing to be thorough about where the expected answer
  came from.
- **A failure may be infrastructure, not judgement.** Read the error before
  concluding the agent got it wrong — a 400 from a provider is not a wrong answer.
