# D56. CI runs golangci-lint

Status: Decided. Amends D12.

The maintainers decided on 2026-11-15 that CI runs golangci-lint with the
linters of the `go-pedantry` skill. This answers question 7 of
`docs/PLAN.md`.

`.golangci.yml` holds the configuration, for golangci-lint 2. In version 2,
gosimple is part of staticcheck, and usetesting takes the place of tenv. CI
installs a fixed version of golangci-lint, and lints the root module and the
modules `compress/xz` and `compress/zstd`. It runs on the linux/amd64 runner
only.

The engine copies libmagic (D34), and some of its code breaks a rule of a
linter on purpose:

1. staticcheck QF1001 is off. The engine keeps each condition of libmagic as
   C writes it, and does not rewrite it by De Morgan's law.
2. An error whose text is the text of libmagic or zlib keeps that text, with a
   `nolint` comment that gives the reason.
3. Each other `nolint` comment gives its reason on the same line.
