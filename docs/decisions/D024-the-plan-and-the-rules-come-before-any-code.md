# D24. The plan and the rules come before any code

Status: Decided.

The maintainers decided on 2026-11-15 that no code of the port is written
before the plan is ready. Two documents come first:

1. [`../PLAN.md`](../PLAN.md), which holds the plan, the reviews and the open
   questions.
2. [`../UPSTREAM.md`](../UPSTREAM.md), which holds the rules for porting
   libmagic and each change that it makes after the base commit (D26).

Code of the port includes Go source, Go tests, the oracle and changes to the
CI workflow. A document, the license, `go.mod` and the files that git reads,
such as `.gitignore`, are not code. Upkeep of the cgo package that users have
today, such as a fix to its archives, is not code of the port.

The plan is ready when its open questions are answered or deferred by a
maintainer. okage D3 made the same rule for the same reason: the rules for
following upstream and for the same output touch every file that is ported,
and a file written before them follows another rule.
