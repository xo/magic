# Backlog

This document holds the work that is known and not done. Each item names
where it came from.

On 2026-09-27, the repository had no open issue, no pull request, open or
closed, and no `TODO` or `FIXME` comment. Every item below came from the CI
history, from the code or from the work that set up D15.

When you finish an item, remove it. If an item needs a decision, it becomes
an open question at the end of `docs/PLAN.md`, and the answer becomes a file
in `docs/decisions/`.

The items before "The binary walker" are about the cgo package that users have
today. Most of them end when the port replaces it (D22). The binary walker
comes after the port (D31).

## CI

1. Every CI run ends as cancelled. The `macos-13` job never gets a runner,
   and GitHub cancels it after 24 hours. On 2026-11-15 the `macos-14` job was
   cancelled before it started too, so no macOS target has CI. Source: runs
   34652233745, 34732334420 and 43657823492 of the `test` workflow. Open
   question 3 in `docs/PLAN.md`.
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
   For `v0.3.0`, the tags waited for CI. Source: open question 8 in
   `docs/PLAN.md`.
7. The script does not know that a rebuild needs a new patch number. A person
   sets `MODULE_PATCH`, and without it the script prints tags that already
   exist. Source: D18 and D13, 2026-11-12.

## The archives

`v0.3.1` did not link on Windows with MinGW gcc (`strndup`), and its macOS
archives called `pipe2`. zig 0.17.1 made file's configure find functions that
the C library of a user does not always have. `v0.4.0` fixed both, and the
build script now tells configure that neither exists on those systems.

A check against this kind of fault: compare the undefined symbols of each new
archive with those of the last release, as was done to find those two. Make
it a step of the build script, so that no one has to remember it.

8. `TestOracle` compares MIME types only, so it did not see the fault of
    D43. Compare the full description with `file` as well. Source: the same
    check.
9. On Windows, file 5.48 passes `&ts.tv_sec` where `readcdf.c` wants a
    `const time_t *`, and the sizes differ. The build keeps the old behavior
    with `-Wno-incompatible-pointer-types`. No sample file reaches the code, so
    no one knows whether the timestamps in a Windows description are wrong.
    Check a newer release of file, and a Compound File with a time property.
    Source: D13, 2026-11-12.

10. A program for linux/arm (32-bit) cannot link the archives with glibc. They
   are built for musl, and they call the names that musl gives to its 64-bit
   time functions, such as `__stat_time64`, which glibc does not have. Every
   release has this. linux/arm64 links with glibc 2.37 and 2.38, and needs
   `strlcpy`, which glibc has from 2.38, and zig at its default glibc lacks.
   Source: the glibc links checked for D43, 2026-11-15.

## The build script

11. The script downloads each tarball and does not compare it with a known
   checksum. The header says "verify tarball checksums yourself". Source:
   `build-libmagic.sh`, line 30.

## The package

12. The errors of the package differ from `go-pedantry`. `errClosed` is not
    exported, so a caller cannot use `errors.Is` on it. `New` returns
    `magic_open failed` without the `magic: ` prefix that the other errors have.
    No error text is in the gerund form that `go-pedantry` asks for. The
    maintainers have not decided which one wins. Source: `magic.go`, found when
    the code was compared with `go-pedantry`.
13. Add a test that reads `go.mod` and fails on a `require` line for a package
    that the maintainers did not approve, as dburl does for every `require`
    line. D28 approves outside packages for xz and zstd only. Source: D14 and
    D28.

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

## Phase 3 of the port

Phase 2 found these. Each one is work for phase 3 (docs/PLAN.md).

17. The port prints no description yet. `mprint` in
    `internal/engine/print.go` prints the text of a rule without its value.
    Phase 3 needs the `printf` formats of C, the dates and the floats. Source:
    `softmagic.c`, `print.c`.
18. The Compound Document reader gives the MIME types, and does not print the
    properties that a description lists: the dates, the counts, the names of
    the application and the list of a `Thumbs.db`. Source: `readcdf.c`,
    `cdf.c` and `cdf_time.c`.
19. The reason that libmagic prints after "Composite Document File V2
    Document" depends on `errno` from an earlier step. A stream of length 0
    sets no `errno`, so the reason can come from the step before it. The port
    does not copy this. It changes no MIME type. Source: `cdf_read_long_sector_chain`
    and `file_trycdf`.
20. The ELF reader changes the mode, and prints nothing. The description of an
    ELF file needs the rest of `readelf.c`.
21. The oracle keeps one cookie for all questions. libmagic keeps the value
    of the last rule and the levels of continuation in the cookie, and a
    string compare reads past the end of a string. So an answer of libmagic
    can depend on the question before it. The port starts each detection
    from zero. No test has found a difference. Source: `ms_value` and `c.li`
    in `file.h`.
22. `File` of the port knows a regular file, an empty file, a directory and a
    file that it cannot read. libmagic also names devices, links, sockets and
    pipes. Source: `fsmagic.c`.
23. The port does not look inside compressed data (D27, D28). Source:
    `compress.c`.
24. No test measures the bytes that the port adds to a program (D38). The
    plan wants one from the first code on.

## The binary walker

The maintainers want magic to do the work of binwalk too: walk a binary file,
such as a firmware image, find the files that are embedded in it at any
offset, and extract them. It comes after the port of libmagic is finished
(D31). On 2026-11-15 the three models of the plan said this about it.

How it works:

1. Do not run the whole rule engine at every byte. Collect the fixed bytes of
   each signature into one matcher for many patterns, such as Aho-Corasick,
   and run the engine only where it finds a candidate. All three models said
   this.
2. Keep a signature set for the walker. The rules of libmagic name a whole
   file, and the signatures of binwalk find a file inside another. Gemini Pro
   suggests translating the signatures of binwalk 2 into the rule format, so
   that one engine reads both. Gemini Pro also said that binwalk 2 uses
   libmagic inside. That is not checked, and neither is the claim that
   binwalk 3 is a rewrite in Rust. Check both, and the license of the
   signatures, before any are copied.
3. Keep three steps apart: find a candidate, find where it ends, and extract
   it. A match does not say how long the embedded file is, so each format needs
   code that finds its end.
4. Work from an `io.ReaderAt`, and stop when a `context.Context` is cancelled.

The formats, from easy to hard:

| Group | Formats | Work |
| --- | --- | --- |
| In the standard library | gzip, zlib, bzip2, zip, tar, ELF headers | find the end, then extract |
| Simple headers | uImage, cpio, common images such as PNG, JPEG and GIF | a small parser each |
| Outside the standard library | xz, raw LZMA, zstd | the packages of D28 |
| Firmware file systems | SquashFS, CramFS, JFFS2, UBIFS | a large parser each. SquashFS has vendor variants. Gemini Pro names `github.com/sylabs/squashfs`, and knows no library for the other three |

Safety limits are needed from the first release: limits on the size of the
output, on the ratio of compression, on the depth of recursion and on time, a
check that no path leaves the output folder, and no device files or links that
point out of it.

The estimates, in engineer-weeks for one experienced Go engineer, after the
port:

| Source | Weeks |
| --- | --- |
| Gemini Pro | 6 to 11, without parsers for CramFS, JFFS2 and UBIFS |
| DeepSeek Pro | 9 to 14 |
| OpenAI Pro | 36 to 76, of which the firmware file systems are 18 to 40 |
| The plan | 23 to 48: scanning 5 to 9, common extraction 5 to 8, SquashFS 3 to 6, CramFS, JFFS2 and UBIFS 10 to 25 |

Before the work starts, the maintainers decide:

1. Which formats, and in which order. SquashFS first is the advice of all
   three models.
2. Whether the signatures of binwalk can be translated, after a check of their
   license.
3. Whether the walker is a package of this module, such as `walk`, or a module
   of its own.
