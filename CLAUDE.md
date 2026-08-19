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

## Installing it

`./install.sh` installs both halves for the current user: the `habitat` binary
via `go install`, and the `habitat` skill into `~/.claude/skills/`.
`--skill-only` / `--bin-only` do one of them; `--link` symlinks the skill back
to this repo so edits here take effect without reinstalling.

`skills/habitat/SKILL.md` is the user-facing guide to the CLI — when to reach
for habitat, the grader and policy reference, the SDK contract, and the cost
discipline. It is documentation of the shipped behaviour, so **a change to the
CLI's flags, graders, policy keys or exit codes means updating it in the same
commit**. It is the thing most likely to drift silently, because nothing
compiles it.

## The server

`habitat serve` is the same binary long-running. It is multi-tenant: a
**project** owns its runs and holds the token a CLI authenticates with, while
people sign in with an account to read reports. Local runs land in an implicit
`local` project, so single-machine use needs no setup and the schema has one
shape everywhere.

Two invariants worth not breaking:

- **It refuses to start unauthenticated off loopback** (`cmd/habitat/serve.go`).
  Binding beyond `127.0.0.1` with no accounts exits 2. A flag could be set
  wrongly; refusing cannot. The failure this prevents is publishing every
  stored prompt and model output.
- **Grading never happens server-side.** The CLI grades, then pushes the
  verdict. One implementation of grading means a run reads the same in the
  terminal and the browser, and a server outage cannot change whether a suite
  passed — which is why a failed push warns but never changes the exit code.

Deployed with Kamal to `habitat.np.lol` on `notnoise-1.server.np.lol`, matching
the other repos here: `config/deploy.yml`, `.kamal/secrets` pulling from
1Password, and a self-hosted native-ARM64 deploy job in `.github/workflows/ci.yml`.
SQLite lives on the `habitat_data` volume, so a deploy never discards history.
Ruby appears in this repo only to pin Kamal's version.

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

## Running it

```
go build -o habitat ./cmd/habitat
./habitat validate --dir examples     # free — calls no target
./habitat run echo --dir examples     # runs the example agent end to end
./habitat serve --dir examples        # browse the runs at :7878
```

`examples/` is a complete working project: `habitat.yml`, one suite, and a
runner declaring a fake agent that costs nothing to execute. It is the
fastest way to see the whole loop, and the thing to check against when the
engine and an SDK disagree.

## Status

MVP engine works end to end: suite parsing and validation, the graders in
[`docs/features-spec.md`](docs/features-spec.md) marked MVP, suite policy,
SQLite persistence, terminal and JSON reports, and the `serve` dashboard.
The Ruby SDK under `sdk/ruby/` registers targets and hands off to its
runner. Not yet built: baseline diffing, the `rubric` grader and its judge,
trace-based graders, JUnit/HTML reports, and generated INV/DIR cases.
