# Coding standards

> Mechanical rules belong in CI or linting. This file holds judgement calls only.

Read before writing code and again at review.

## The best part is no part

- Does it need to exist? Push back on requirements.
- Prefer deletion. Remove dead code, redundant validators, and speculative options first, then build.
- Use components already in this codebase instead of rebuilding them.
- YAGNI ("You Aren't Gonna Need It"): do not build features or add functionality until they are needed.
- When evolving a system, remove complexity first, then build. Default to subtraction.

## Strategic coding

- Prioritise long-term system design, maintainability, and low complexity.

## Writing for humans

- Any part of the UI, docs, or system that a human interacts with must follow unslop rules.

## Laziness protocol

- Minimise the diff. Make the smallest change that solves the problem. Fewer lines beat boilerplate.

## Make operations idempotent

- Derive each step from the state that exists, not from what the previous run meant to do. Read the current state, compare it to the goal, and apply only the difference.
- Anything a run leaves behind (a lock, a temporary file, a half-written record, a claimed job) must be recognisable as stale by the next run, using evidence that holds wherever the previous run could have stopped. Write to a temporary path and rename it into place.
- The test: what happens if this runs twice? What if the previous run crashed at any point? If the answer depends on what was left behind, add a reconciliation step.

## Data

- Get the data shape right before writing logic.
- Document the data model and agree on it with the human before implementation.

## Prove it works

- Verify every output by checking the real thing directly: run the feature, read the actual value, inspect the diff. Proxies (file mtimes, self-reports, cached output, "it compiles") do not count.
- Script the check when you can. A deterministic script that reruns the comparison beats a one-time look.

## Deep modules

- Design **deep modules**: put substantial behaviour behind a small interface, placed at a clean seam, testable through that interface. Use this language and these principles when designing or restructuring code. The aim is simpler call sites, locality for maintainers, and testability for everyone.
