# Backlog

This document holds the work that is known and not done. Each item names
where it came from.

On 2026-09-27, the repository had no open issue, no pull request, open or
closed, and no `TODO` or `FIXME` comment. Every item below came from the CI
history, from the code or from the work that set up D15.

When you finish an item, remove it. If an item needs a decision, the
decision goes in `docs/PLAN.md`.

## CI

1. Every CI run ends as cancelled. The `macos-13` job never gets a runner,
   and GitHub cancels it after 24 hours. The other four jobs pass. Source:
   runs 34652233745 and 34732334420 of the `test` workflow. Open question 3
   in `docs/PLAN.md`.
2. No CI job builds linux/armv7. `dedf132` removed the `cross` job, which was
   the only one that linked it. Source: D12. Open question 2 in
   `docs/PLAN.md`.
3. CI does not run `gofmt`. `dedf132` removed the step. Source: D12. Open
   question 4 in `docs/PLAN.md`.
4. The repository has no `.golangci.yml`, and CI runs no linter. Source:
   found while this setup was written. Open question 7 in `docs/PLAN.md`.

## The build script

5. `XZ_FALLBACK` in `build-libmagic.sh` says 5.8.3. The committed archives
   hold xz 5.8.4, and the comment on the fallbacks says that they name what
   the archives were built from. Set it to 5.8.4. Source: `a629036` changed
   every `liblzma.a`, and `strings` on each archive gives 5.8.4. Found while
   D13 was written.
6. The script downloads each tarball and does not compare it with a known
   checksum. The header says "verify tarball checksums yourself". Source:
   `build-libmagic.sh`, line 30.

## The package

7. The errors of the package differ from `go-pedantry`. `errClosed` is not
   exported, so a caller cannot use `errors.Is` on it. `New` returns
   `magic_open failed` without the `magic: ` prefix that the other errors
   have. No error text is in the gerund form that `go-pedantry` asks for.
   Ken did not decide yet which one wins. Source: `magic.go`, found when the
   code was compared with `go-pedantry`.
8. If Ken decides D14, add a test that reads `go.mod` and fails on a
   `require` line, as dburl does. Source: open question 1 in `docs/PLAN.md`.

## The tests

9. The case names and 16 of the 21 files in `testdata/` came from the tests
   of `github.com/gabriel-vasile/mimetype`, which is under the MIT license.
   The repository does not carry its copyright notice. Two case names, "json
   issue#239" and "jar in zip #639", cite issues of mimetype and do not name
   it. Source: `magic_test.go` and `testdata/`, compared with mimetype
   v1.4.15. Open question 5 in `docs/PLAN.md`.
10. Some cases in `mimeTests` repeat. "kml 2.2" and "ogg" are each the
    name of two cases, so `go test` reports the second one as `ogg#01`.
    "rpm" has the same input as "rpm 1". "svg with comment prefix" has the
    same input as "svg no xml header" and holds no comment. Source:
    `magic_test.go`.
11. `TestVersion` logs the version and fails on nothing. Source:
    `magic_test.go`.
