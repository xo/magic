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

5. freebsd/amd64 links and has not run. The program and the test binary
   linked with `zig cc`, and the build host cannot run them. GitHub has no
   hosted FreeBSD runner. The darwin archives were compiled and not linked on
   the build host, so the macOS jobs do the link. If a job fails, fix it or
   take the target out. Source: D19.

## Modules and tags

6. A rule for CI before a tag is not decided. resvg tagged before CI finished.
   For `v0.3.0`, the tags waited for CI. Source: open question 8 in `docs/PLAN.md`.
7. The script does not know that a rebuild needs a new patch number. A person
   sets `MODULE_PATCH`, and without it the script prints tags that already
   exist. Source: D18 and D13, 2026-11-12.

## The archives

8. A program that links the Linux archives with glibc gets a wrong description
   for Netpbm files. On linux/amd64, a musl link matches `file` on all 21
   sample files, and a glibc link differs on `pbm.pbm`, `pgm.pgm` and
   `ppm.ppm`. arm and arm64 matched `file` when linked with musl, and no one
   tried a glibc link of them. The darwin and freebsd archives were not run.
   The cause is not confirmed. Source: found when the zlib 1.4 rebuild was
   checked. Open question 9 in `docs/PLAN.md`.
9. `TestOracle` compares MIME types only, so it did not see item 8. Compare
    the full description with `file` as well. Source: the same check.
10. On Windows, file 5.48 passes `&ts.tv_sec` where `readcdf.c` wants a
    `const time_t *`, and the sizes differ. The build keeps the old behavior
    with `-Wno-incompatible-pointer-types`. No sample file reaches the code, so
    no one knows whether the timestamps in a Windows description are wrong.
    Check a newer release of file, and a Compound File with a time property.
    Source: D13, 2026-11-12.

## The build script

11. The script downloads each tarball and does not compare it with a known
   checksum. The header says "verify tarball checksums yourself". Source:
   `build-libmagic.sh`, line 30.

## The package

12. The errors of the package differ from `go-pedantry`. `errClosed` is not
   exported, so a caller cannot use `errors.Is` on it. `New` returns
   `magic_open failed` without the `magic: ` prefix that the other errors
   have. No error text is in the gerund form that `go-pedantry` asks for.
   Ken did not decide yet which one wins. Source: `magic.go`, found when the
   code was compared with `go-pedantry`.
13. If Ken decides D14, add a test that reads `go.mod` and fails on a
   `require` line, as dburl does. Source: open question 1 in `docs/PLAN.md`.

## The tests

14. The case names and 16 of the 21 files in `testdata/` came from the tests
   of `github.com/gabriel-vasile/mimetype`, which is under the MIT license.
   The repository does not carry its copyright notice. Two case names, "json
   issue#239" and "jar in zip #639", cite issues of mimetype and do not name
   it. Source: `magic_test.go` and `testdata/`, compared with mimetype
   v1.4.15. Open question 5 in `docs/PLAN.md`.
15. Some cases in `mimeTests` repeat. "kml 2.2" and "ogg" are each the
    name of two cases, so `go test` reports the second one as `ogg#01`.
    "rpm" has the same input as "rpm 1". "svg with comment prefix" has the
    same input as "svg no xml header" and holds no comment. Source:
    `magic_test.go`.
16. `TestVersion` logs the version and fails on nothing. Source:
    `magic_test.go`.
