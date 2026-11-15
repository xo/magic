# D57. CI passes before a tag

Status: Decided.

The maintainers decided on 2026-11-15 that a commit gets a tag only after
every job of CI passes on it. This answers question 8 of `docs/PLAN.md`.

`v0.3.1` was tagged while CI was down, and CI found its fault on Windows four
days later. resvg tagged its modules before CI finished on the commit. The
releases `v0.5.0` and `v0.5.1` waited for CI.

The rule holds for every tag: the root module and each module of its own,
such as `compress/xz`. A module proxy caches a tag, so a tag that is published
cannot move (D39).
