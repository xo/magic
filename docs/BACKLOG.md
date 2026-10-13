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

5. Two targets link and have not run. The program and the test binary for
   windows/arm64 and freebsd/amd64 linked with `zig cc`. The build host cannot
   run either. The first run of the `windows/arm64` job on `windows-11-arm` is
   the first test of windows/arm64. That job uses `zig cc` and
   `mlugg/setup-zig@v2`, and no run has used either yet. GitHub has no hosted
   FreeBSD runner. The darwin archives were compiled and not linked on the build
   host, so the macOS jobs do the link. If a job fails, fix it or take the
   target out. Source: D16 and D19.
6. Only `zig cc` links the windows/arm64 archives in any test. resvg reports
   that its Windows arm64 archive needs a clang based compiler such as
   llvm-mingw, because of libunwind. magic has no C++ or Rust code, so it may
   not apply, but no one tested llvm-mingw. Source: the message of resvg of
   2026-10-14.

## Modules and tags

7. Nothing tested the module layout against real tags. The tests use the
   `replace` lines. After the tags exist, make an empty module, run `go get`
   for the root tag, build a program that calls `magic.New`, and make sure
   that Go downloaded the archives of one platform only. Source: D17.
8. A rule for CI before a tag is not decided. resvg tagged before CI finished.
   For `v0.3.0`, the tags waited for CI. Source: open question 8 in `docs/PLAN.md`.
9. The script prints a tag with the patch number `.0` and does not know about
   a rebuild at the same version of file. Source: D18.

## The build script

10. The script downloads each tarball and does not compare it with a known
   checksum. The header says "verify tarball checksums yourself". Source:
   `build-libmagic.sh`, line 30.

## The package

11. The errors of the package differ from `go-pedantry`. `errClosed` is not
   exported, so a caller cannot use `errors.Is` on it. `New` returns
   `magic_open failed` without the `magic: ` prefix that the other errors
   have. No error text is in the gerund form that `go-pedantry` asks for.
   Ken did not decide yet which one wins. Source: `magic.go`, found when the
   code was compared with `go-pedantry`.
12. If Ken decides D14, add a test that reads `go.mod` and fails on a
   `require` line, as dburl does. Source: open question 1 in `docs/PLAN.md`.

## The tests

13. The case names and 16 of the 21 files in `testdata/` came from the tests
   of `github.com/gabriel-vasile/mimetype`, which is under the MIT license.
   The repository does not carry its copyright notice. Two case names, "json
   issue#239" and "jar in zip #639", cite issues of mimetype and do not name
   it. Source: `magic_test.go` and `testdata/`, compared with mimetype
   v1.4.15. Open question 5 in `docs/PLAN.md`.
14. Some cases in `mimeTests` repeat. "kml 2.2" and "ogg" are each the
    name of two cases, so `go test` reports the second one as `ogg#01`.
    "rpm" has the same input as "rpm 1". "svg with comment prefix" has the
    same input as "svg no xml header" and holds no comment. Source:
    `magic_test.go`.
15. `TestVersion` logs the version and fails on nothing. Source:
    `magic_test.go`.
