# Habitat — Product Spec

## What it is

Habitat is a self-hosted evaluation engine for grading code whose output can't
be asserted byte-for-byte — LLM agents, ranking/ML pipelines, anything where
the same input can legitimately come back phrased two different ways on two
different runs. It runs hand-verified cases against real application code,
grades each execution, and keeps a browsable history of every run so a team
can answer "did this change help or hurt" without digging through log files
or a one-off report someone happened to save.

## The problem

Deterministic test suites assert exact output: same input, same output,
every time. Code that calls a real model doesn't behave that way — mocking
the model away only tests the plumbing around it, not whether the answer is
actually right. Teams building on top of non-deterministic components need a
separate discipline from their normal test suite: hand-verified cases,
tolerance for repeated attempts, and a grading step that checks the parts of
the output that are supposed to be stable (a structured field, a
classification, a threshold) rather than prose that legitimately varies
between otherwise-correct runs.

Today that usually means a pile of ad hoc scripts nobody fully trusts, no
shared history across the team, and "did we get better or worse" answered
from memory instead of data — and it's usually tied to one language, because
the harness was written inside one particular app.

## What habitat does

- Runs a suite of hand-verified cases against a **target** — real
  application code, invoked in whatever language it's written in.
- Grades every execution independently with one or more **graders** —
  deterministic checks first (structured-field match, schema validation,
  tool-trace assertions), model-judged rubrics later.
- Repeats a case multiple times when the target is non-deterministic, and
  reports the fraction of repetitions that passed, not just a single
  pass/fail.
- Applies suite-level **policy** on top of individual case results — e.g.
  "every critical case must pass," "average cost must stay under $X,"
  "pass rate must not regress more than 5% versus the last baseline."
- Stores every run centrally and serves it back as a **navigable report** —
  suite list, run history, per-case drill-down, run-over-run comparison —
  instead of a static file someone has to go find and share.
- Stays out of the way of the normal test suite: it's invoked deliberately,
  not on every commit, because an execution against a real target can cost
  real money and take real time.
- Is not tied to one language. The engine and the suite format are
  implemented once, in Go; anything that can make an HTTP call can act as a
  runner for it, starting with a first-party Ruby SDK.

## Who it's for

Engineers who ship a feature backed by a non-deterministic call (an LLM, a
ranking model, anything probabilistic) and need repeatable, reviewable
evidence that a prompt, model, or pipeline change didn't regress a behavior
someone already verified by hand — plus a shared place the whole team can
look at that evidence, not just the person who happened to run it locally.

## How it's different from a normal test framework

| | Unit / integration tests | Habitat |
|---|---|---|
| Assertion | Exact | Graded, with tolerance for phrasing |
| Attempts per case | 1 | N repetitions; pass-rate reported |
| Run cadence | Every commit, in CI | On demand — cost-aware |
| Result lives | CI logs, ephemeral | Persisted centrally, browsable |
| Target language | Same process, same language as the test runner | Any language, reached over HTTP |

## Non-goals (v1)

- Not a replacement for the deterministic test suite — it complements it.
- Not a prompt-engineering playground or an LLM chat UI.
- Not an automatic case generator. Stretching thin ground truth with
  generated variants is a deliberate, human-reviewed workflow (a later
  phase), not a button that manufactures cases for you.
- Not multi-tenant SaaS. One team, self-hosted, one store.

## Success looks like

A developer changes a prompt, runs `habitat run <suite>`, sees a pass-rate
in the terminal immediately, and that same run shows up in `habitat serve`'s
dashboard a moment later for a teammate to review — with a one-click
comparison against the last known-good baseline.
