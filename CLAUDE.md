# habitat

Self-hosted evaluation engine for grading non-deterministic code — LLM
agents, ranking pipelines, anything that can't be asserted byte-for-byte —
against hand-verified cases, independent of the language the code under
test is written in. See [`docs/product-spec.md`](docs/product-spec.md) for
what it does and why, [`docs/features-spec.md`](docs/features-spec.md) for
the concrete feature set and phasing, and
[`docs/architecture-spec.md`](docs/architecture-spec.md) for how it's built.

## Layout

- `cmd/habitat/` — CLI entrypoint. One Go binary; `run`, `validate`,
  `list`, `show`, and `serve` are subcommands of it, not separate programs.
- `internal/` — the engine: suite parsing, graders, policy evaluation, the
  local ingest server `run` starts, the SQLite store, report rendering.
  There is exactly one implementation of the suite format and of grading
  logic, and it lives here.
- `pkg/protocol/` — the versioned JSON wire types every SDK, in every
  language, implements against.
- `sdk/ruby/` — the first-party Ruby SDK gem. A target registry plus a thin
  HTTP client that posts raw execution results to whichever `habitat`
  process is listening. It never parses suite YAML and never grades
  anything — that stays in Go, on purpose, so a second-language SDK is a
  small client to write, not a second place grading logic can drift.
- `examples/suites/` — example suite YAML used in the docs and for
  dogfooding.

## Linting

`./lint.sh` is the check — not bare `golangci-lint run`. It runs
golangci-lint against [`.golangci.yml`](.golangci.yml) and adds a whole-file
length limit (600 lines, tests exempt), which golangci-lint has no linter
for: every length rule it ships measures a function or a single line.
`./lint.sh --fix` applies what the linters can fix themselves.

`.golangci.yml` is a readability budget on top of golangci-lint's default
correctness set. Its thresholds are set where a reader starts to struggle
rather than where the code happens to sit, so tightening one should mean
fixing code rather than editing the file. Both files are the standard shared
across these Go projects — keep changes to the shared `linters`/`settings`
blocks in sync rather than letting this repo drift; `exclusions.rules` is the
part that is legitimately repo-specific.

Note that golangci-lint reports only one issue per line by default, so a
second linter firing on the same line stays hidden until the first is fixed.

## Dev process

No CI exists yet. Once it does, the rule carried over from other repos
here applies: **a push isn't done until CI is** — watch the run to
completion (`gh run list --commit <full SHA> --json status,conclusion`)
rather than assuming it passed, and read a red run before re-running it.
Use the full SHA; a short one silently matches nothing and the wait exits
immediately, which looks like a pass.

Commit messages are short, descriptive sentences in the imperative,
describing the effect of the change — not a `type: subject` prefix. E.g.
"Grade structured fields instead of prose," not "feat: improve grading."

## Status

Pre-implementation. `docs/*.md` is the current source of truth; nothing
under `cmd/` or `internal/` exists yet.
