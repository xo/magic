# D53. CI runs on the current macOS runners

Status: Decided. Amends D12.

The maintainers decided on 2026-11-15 to keep the macOS runners that CI
moved to with the release v0.5.0. This answers question 3 of `docs/PLAN.md`.

GitHub retired `macos-13` and `macos-14`. The job on `macos-14` stopped at
once, and the job on `macos-13` waited until the run was cancelled. CI now
tests macOS amd64 on `macos-15-intel` and macOS arm64 on `macos-latest`.
