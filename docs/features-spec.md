# Habitat — Features Spec

Concrete feature set, phased. MVP is the smallest thing that gives a team a
real, shared, browsable eval history end to end; later phases add the
features that make it a serious daily tool. See `architecture-spec.md` for
how each of these is implemented and communicates over the wire.

## Suite format

One YAML file per suite. Parsed and validated by the Go engine only — no
other component (including every language SDK) parses or re-implements this
format, so there is exactly one place suite-format bugs can live.

| Field | Level | Meaning |
|---|---|---|
| `name` | suite | Display name shown in reports. |
| `target` | suite | Key of a target the runner has registered. |
| `default_repetitions` | suite | Attempts per case unless a case overrides it. |
| `policy` | suite | Optional suite-level pass/fail rules — see Policies. |
| `id` | case | Unique within the suite. Shown in every report. |
| `title` / `description` | case | Prose only. Never affects grading or the suite's digest, so editing them never invalidates a baseline comparison. |
| `critical` | case | If `true`, this case failing fails the suite outright when the policy sets `critical_cases_must_pass`. |
| `tags` | case | Free-form labels, filterable in the dashboard. |
| `input` | case | Whatever the target's `call` expects — passed through opaquely by the engine. |
| `repetitions` | case | Overrides `default_repetitions` for this one case. |
| `expectations` | case | A list of grader configs — each a `type` plus that grader's own options. |

A suite is a list of cases, not a single input/output pair — one suite can
cover several scenarios, each with its own id and repetition count.

## Graders

Each entry in a case's `expectations` names one grader. A case can carry
several; a `no_error` check plus a handful of structured-field checks is the
default shape.

| Grader | Passes when… | Phase |
|---|---|---|
| `no_error` | The target executed without an error. | MVP |
| `exact_match` | The output (or the value at `path`) equals `value` exactly. | MVP |
| `includes` | The output contains `value` — substring for strings, membership for arrays. | MVP |
| `state_match` | The value at dot-separated `path` in the execution's final state equals `value`. The default choice for grading a structured extraction — grade the fields, not the free-text prose, since wording legitimately varies between otherwise-correct runs. | MVP |
| `json_schema` | The output validates against a small JSON Schema subset (`type`, `required`, `properties`, `items`, `contains`, `not`, `enum`, `const`, numeric/string/array bounds, `pattern`). `contains`/`not contains` is how presence/absence inside an unordered list is asserted. | MVP |
| `maximum_duration` | Execution took at most `maximum_ms` milliseconds. | MVP |
| `maximum_cost` | Execution's reported cost is at most `maximum_usd`. | MVP |
| `required_tool` / `forbidden_tool` / `maximum_tool_calls` | Trace-based checks over `events` — for targets that call tools. | Phase 2 |
| `rubric` | A configured model judge scores the output against `criteria`, and a suite-author-set `threshold` (not the judge's own opinion) decides pass/fail. Fails cleanly, not the whole run, if no judge is configured. | Phase 2 |

### Choosing what a grader reads — `source`

`exact_match`, `includes` and `json_schema` read the execution's `output` by
default, and `state_match` reads `final_state`. An agent whose answer is
prose keeps its gradeable structure in `final_state`, so those three also
accept `source: final_state` to point at it — otherwise such a target would
have to duplicate its structured state into its prose just to be assertable.
The default stays `output`, and an unrecognised `source` is a load-time
error.

```yaml
- type: json_schema
  source: final_state
  path: action_types
  schema:
    type: array
    contains: { type: string, const: add_exercise }
```

An unsupported or misspelled grader key or option raises a validation error
at suite-load time — a check the author believes they configured is exactly
the failure mode this guards against.

## Policies

Suite-level rules layered on top of individual case results. Left
unconfigured, the implicit policy already requires every case to pass every
repetition (`minimum_pass_rate: 1.0`).

| Key | Fails the suite when… | Phase |
|---|---|---|
| `minimum_pass_rate` (default `1.0`) | Overall pass rate is below this fraction. | MVP |
| `critical_cases_must_pass` | Any case marked `critical: true` failed, regardless of overall pass rate. | MVP |
| `maximum_error_rate` | The share of executions that errored exceeds this. | MVP |
| `maximum_average_cost` | Average reported cost per execution exceeds this many USD. | MVP |
| `maximum_average_duration_ms` | Average execution duration exceeds this many milliseconds. | MVP |
| `maximum_regression` | Baseline-relative only — pass rate dropped more than this versus a `--baseline` run. | Phase 2 |
| `minimum_average_rubric_score` | Average score across every rubric grader in the run is below this. Vacuous if the suite has no rubric expectations. | Phase 2 |
| `no_forbidden_tool_violations` | Any `forbidden_tool` expectation was violated anywhere in the run. | Phase 2 |

## Repetitions

Each case runs `repetitions` times (suite default, case override, or
`--repetitions=N` on the command line). A case's own status requires every
repetition to pass; the `X/Y repetitions passed` fraction stays visible
even when the case fails outright, so a consistently wrong case reads
differently from a merely flaky one.

## Ruby SDK (first-party, MVP)

A gem, not an app-specific vendor drop. Its entire job:

- **Target registry** — the app registers a target by key to something
  responding to `call(input:, context:) -> Habitat::Result`, mirroring the
  suite's `target:` field. A target never decides pass/fail; it only
  normalizes what happened.
- **`Habitat::Result`** — `output`, `final_state`, `events`, `usage`,
  `duration_ms`, `error`. Same shape regardless of grader; `state_match`
  reads `final_state`, `json_schema`/`includes`/`rubric` read `output`,
  trace graders read `events`.
- **Runner loop** — on start, fetches the list of `{case_id, repetition_index,
  input}` executions to perform from whichever `habitat` process launched
  it (via `HABITAT_URL`), calls the matching registered target for each,
  and posts the resulting `Habitat::Result` back. It never parses the suite
  YAML, never applies a grader, never evaluates policy.

Later-phase SDKs for other languages (Python, TypeScript, …) implement the
same three responsibilities against the same wire protocol — no engine
change required to add one.

## CLI

Single binary, `habitat`, all subcommands of it — the runner engine *is*
the CLI process, not a thing it shells out to separately.

| Command | Effect | Phase |
|---|---|---|
| `habitat run <suite> [-- <runner-command>]` | Parses and validates the suite, starts a local ingest server, runs the given runner command (e.g. `bundle exec ruby run.rb`) as a subprocess, grades every execution it posts back, evaluates policy, persists the run, prints a report. | MVP |
| `habitat validate [NAME...]` | Validates one or all suites. Free — makes no calls to any target. | MVP |
| `habitat list` | Lists suites found in the configured suite path. | MVP |
| `habitat show <run-id>` | Prints a previously persisted run. | MVP |
| `habitat serve [--port] [--db]` | Starts the long-running HTTP dashboard over a SQLite store — suite list, run history, run detail, drill-down. | MVP |
| `habitat init` | Scaffolds a starter suite file and SDK registration snippet. | Phase 2 |

### Options for `run`

| Option | Effect | Phase |
|---|---|---|
| `--repetitions=N` | Override the suite's repetition count. | MVP |
| `--format=terminal\|json` | Report format. Default `terminal`. | MVP |
| `--output=PATH` | Write the report to a file instead of stdout. | MVP |
| `--db=PATH` | SQLite file to persist into. Defaults to `.habitat/habitat.db`. | MVP |
| `--server=URL` | After finalizing locally, also report the finished run to a habitat server. | MVP |
| `--no-push` | Never report this run to a server. | MVP |
| `--baseline=RUN_ID\|PATH` | Compare against a previous run. | Phase 2 |
| `--format=junit\|html` | Additional report formats. | Phase 2 |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Completed — every case passed. |
| 1 | Completed — at least one case failed. |
| 2 | Invalid configuration or suite — nothing executed. |
| 3 | A framework error during execution, or the run completed but persistence failed. |

## Reports

- **Terminal** (default) — one line per case, pass/fail, and the `X/Y`
  repetitions-passed fraction; a summary line for the suite. MVP.
- **JSON** — the full structured payload; also the exact shape `--baseline`
  reads back in. MVP.
- **JUnit XML** — for CI dashboards that already understand that format.
  Phase 2.
- **HTML** — self-contained, static, shows exactly what the dashboard's
  run-detail page shows; pasteable as a one-off artifact when someone needs
  a report outside the dashboard. Phase 2.

## Web dashboard (`habitat serve`)

- Suite list, with each suite's most recent pass rate.
- Run history per suite — pass rate over time.
- Run detail — every case, its status, its repetitions-passed fraction.
- Case detail — drill into individual repetitions, including which
  expectation(s) failed.
- Run-over-run comparison view — the browser equivalent of `--baseline`,
  cases classified improved / regressed / unchanged / new / missing.
  Phase 2.
- Tag-based filtering across suites. Phase 2.

## Persistence

Every `habitat run` persists by default — this is not an optional add-on
the way it might be in a single-process harness, since a shared, browsable
history is the point of having a server component at all. Local runs write
to a SQLite file (`.habitat/habitat.db` by default); `--push` (Phase 2)
additionally uploads the finished run to a shared `habitat serve` instance,
which is how CI and every developer's local runs end up in one place.

## Cost reporting

Cost comes entirely from a target's own `usage` on its `Habitat::Result` —
the engine computes nothing on a target's behalf. When at least one
execution in a run reports a cost, reports gain average/total cost rows at
case and suite level; when none do, cost fields are omitted rather than
shown as a misleading $0.00.

## Deferred: generated cases (Phase 3)

Stretching a suite grounded in one or two hand-verified cases with
generated variants — invariance transforms whose expectations are copied
verbatim from the seed, and directional transforms whose expectations are
recomputed by a stated, checkable formula — is valuable but explicitly out
of MVP and Phase 2 scope. It's a human-reviewed authoring workflow, not an
engine feature, and it depends on the core run/grade/report loop already
being solid.

## Deferred: additional language SDKs (Phase 3)

Python and TypeScript SDKs, implementing the same target-registry +
runner-loop contract as the Ruby SDK, against the same versioned wire
protocol.
