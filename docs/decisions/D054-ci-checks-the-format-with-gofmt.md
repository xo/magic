# D54. CI checks the format with gofmt

Status: Decided. Amends D12.

The maintainers decided on 2026-11-15 that CI fails when `gofmt -l .` prints
a file. This answers question 4 of `docs/PLAN.md`. `dedf132` had removed the
step.

The step runs on the linux/amd64 runner only, because gofmt gives the same
answer on every runner.
