# Plan

This document holds the plan for `magic`, then every decision that shapes it,
then the open questions for Ken.

## Purpose

`magic` lets a Go program identify the type of a file or a buffer with
libmagic, the library of the `file` command. A program that imports it needs
Go and a C compiler, and nothing else. It does not need libmagic on the
system and it does not need a magic database on the system, because the
repository carries both (D1, D3).

## Current state

On 2026-09-27, `magic` is at commit `a629036` and has no release tag. It
wraps libmagic 5.48 from file 5.48, and links it with zlib 1.3.2, bzip2 1.0.8,
xz 5.8.4 and zstd 1.5.7. The Windows archives also hold libgnurx 2.5.1.

The package is one file, `magic.go`, of 312 lines. It has these parts:

- `New` opens a handle and loads the embedded database.
- `Buffer` and `File` identify a buffer or a file with the flags of the
  handle.
- `BufferWith`, `FileWith`, `BufferTypes` and `FileTypes` identify with other
  flags and do not change the flags of the handle (D10).
- `Flags` and `SetFlags` read and change the flags of the handle.
- `Close` releases the handle.
- `Version` and `VersionInt` report the libmagic version.
- The flag constants mirror the `MAGIC_*` constants of `magic.h`.

A handle is safe to share across goroutines (D9).

The tests in `magic_test.go` pass on linux/amd64 with Go 1.27.1. CI runs them
on five hosted runners. Four of them pass. The macOS amd64 job never starts,
so every run ends as cancelled. `docs/BACKLOG.md` holds that item and the
other known work.

## Direction

The history records two directions:

1. Keep the archives on the newest stable release of each dependency (D13).
2. Keep one handle safe to share across goroutines (D9).

Nothing else is recorded. The questions at the end of this document name the
choices that are not made yet.

## Decisions

Each decision that shapes `magic`, in the order that it was made. The entries
are append only. A decision is never edited to change its conclusion. When a
later decision changes one, the older entry keeps its text, and its heading
names the decision that changed it.

Each heading ends with a status. `Decided` means that the history shows that
Ken chose it. `Proposed` means that the code follows it and no commit, comment
or document says that Ken chose it. A decision that changes an earlier one
says `Amends Dn`, and the earlier one says `Amended by Dn`.
`TestTheDecisionIndexIsComplete` and `TestAnAmendmentPointsBothWays` hold
this.

A bare number, such as D3, names a decision in this document. A decision of
another repository is named with that repository, such as dbmeta D110.

D1 to D14 were written on 2026-09-27 from the history of the repository, from
`7b14d3f` (2026-09-04) to `a629036` (2026-09-13). Each gives the commit, what
changed and the reason. The reason comes from a commit message or a comment.
If neither gives a reason, the entry says "Reason not recorded".

| Decision | Title | Status |
| --- | --- | --- |
| [D1](#d1-the-package-links-static-libmagic-archives-that-the-repository-carries-decided) | The package links static libmagic archives that the repository carries | Decided |
| [D2](#d2-build-libmagicsh-cross-compiles-the-archives-with-zig-cc-decided) | build-libmagic.sh cross-compiles the archives with zig cc | Decided |
| [D3](#d3-the-magic-database-is-embedded-and-one-copy-serves-every-target-decided) | The magic database is embedded, and one copy serves every target | Decided |
| [D4](#d4-libmagic-reads-compressed-files-through-zlib-bzip2-xz-and-zstd-decided) | libmagic reads compressed files through zlib, bzip2, xz and zstd | Decided |
| [D5](#d5-the-api-follows-the-libmagic-c-api-decided) | The API follows the libmagic C API | Decided |
| [D6](#d6-a-handle-is-not-safe-for-concurrent-use-decided-amended-by-d9) | A handle is not safe for concurrent use | Decided. Amended by D9 |
| [D7](#d7-a-closed-handle-reports-an-error-and-never-reaches-c-decided) | A closed handle reports an error and never reaches C | Decided |
| [D8](#d8-the-code-is-under-the-mit-license-decided) | The code is under the MIT license | Decided |
| [D9](#d9-a-handle-is-safe-to-share-across-goroutines-decided-amends-d6) | A handle is safe to share across goroutines | Decided. Amends D6 |
| [D10](#d10-a-lookup-can-use-other-flags-and-leave-the-flags-of-the-handle-alone-decided) | A lookup can use other flags and leave the flags of the handle alone | Decided |
| [D11](#d11-the-tests-expect-what-libmagic-548-reports-and-file1-is-the-reference-decided) | The tests expect what libmagic 5.48 reports, and file(1) is the reference | Decided |
| [D12](#d12-ci-builds-and-tests-only-on-hosted-runners-decided) | CI builds and tests only on hosted runners | Decided |
| [D13](#d13-the-script-builds-against-the-newest-stable-release-of-each-dependency-decided) | The script builds against the newest stable release of each dependency | Decided |
| [D14](#d14-the-module-has-no-go-dependencies-proposed) | The module has no Go dependencies | Proposed |
| [D15](#d15-magic-is-set-up-for-coding-agents-as-every-xo-repository-is-decided) | magic is set up for coding agents as every xo repository is | Decided |

### D1. The package links static libmagic archives that the repository carries. Decided.

Commit `7b14d3f` (2026-09-04).

`magic.go` has one `#cgo LDFLAGS` line for each target. Each line points at
`libmagic/<target>/` and links `libmagic.a` with the four compression
libraries. The archives are committed. There are six targets: linux/amd64,
linux/arm64, linux/arm (armv7), darwin/amd64, darwin/arm64 and
windows/amd64.

The reason is in two comments. The workflow says that the archives are
static, "so there is nothing to install". `build-libmagic.sh` says "These
archives get committed", and it filters out pre-release tags for that reason
(D13).

A consequence: a platform that has no line in `magic.go` does not build.

### D2. build-libmagic.sh cross-compiles the archives with zig cc. Decided.

Commit `7b14d3f` (2026-09-04). Commit `a629036` changed how the versions are
chosen (D13).

One script on one machine builds the archives for all six targets. `zig cc`
is the C compiler for every target. The Linux targets use musl. The script
first builds a native copy of `file`, because the build of file runs `file`
to compile `magic.mgc`, and the cross-compiled one does not run on the host.

The comments in the script give the reason for each workaround, such as the
bzip2 Makefile that ignores `CC` and the zlib `configure` that does not work
for MinGW.

### D3. The magic database is embedded, and one copy serves every target. Decided.

Commit `7b14d3f` (2026-09-04).

`//go:embed libmagic/magic.mgc` puts the database in the Go binary as
`MagicDB`. `New` copies it into C memory once for each handle and loads it
with `magic_load_buffers`. The package never reads a magic file from the
system.

The comment in `New` gives the reason for the copy. `magic_load_buffers`
keeps the pointer for the life of the handle, and it can byte-swap the
contents in place. A Go pointer cannot be kept past the call, and the
embedded bytes can be in a read-only section.

`libmagic/magic.h` and `libmagic/magic.mgc` are shared by all targets. The
comment on `MagicDB` says that the format does not depend on byte order.
`share_once` in the script prints a warning if one target builds a different
file.

### D4. libmagic reads compressed files through zlib, bzip2, xz and zstd. Decided.

Commit `7b14d3f` (2026-09-04). Commit `dbcd068` (2026-09-12) added
`-lshlwapi` to the Windows line.

The script configures file with `--enable-zlib --enable-bzlib --enable-xzlib
--enable-zstdlib` and with `--disable-lzlib --disable-libseccomp`. On
Windows, libgnurx gives libmagic the POSIX regex functions that MinGW does
not have. `src/magic.c` in file 5.48 includes `shlwapi.h`, so the Windows
line also links `shlwapi`.

The comment on `GNURX_FALLBACK` says that libgnurx is under LGPL 2.1, "unlike
everything else here". Reason not recorded for the choice of the four
compression libraries.

### D5. The API follows the libmagic C API. Decided.

Commit `7b14d3f` (2026-09-04). Commit `7d90219` (2026-09-04) added the
`-mime` flag to `cmd/file`.

`New` takes an `int` of flags. The exported constants mirror the `MAGIC_*`
constants and take their values from `magic.h`. `Buffer` and `File` return
the string that libmagic returns. An error from libmagic carries the name of
the C function and the text of `magic_error`.

`cmd/file` is an example. The commit message of `7d90219` calls it "cmd/file
example". It prints the type of each file that it gets, and `-mime` selects
`MimeType`.

### D6. A handle is not safe for concurrent use. Decided. Amended by D9.

Commit `7b14d3f` (2026-09-04).

The first comment on `Magic` said that a `magic_t` holds mutable state for
each lookup and is not safe for concurrent use. It told the caller to give
each goroutine its own handle, or to guard one with a mutex.

### D7. A closed handle reports an error and never reaches C. Decided.

Commit `7b14d3f` (2026-09-04). Commit `dbcd068` (2026-09-12) extended it.

`Close` releases the handle and the copy of the database, and a second call
does nothing. A finalizer calls `Close` if the caller does not. The comment
calls it "Backstop only".

At first, `Buffer` and `File` returned an error on a closed handle.
`dbcd068` made every method return `errClosed`, and made `Close` take the
lock of the handle (D9). The comment on `Close` gives the reason: a method
that runs at the same time as `Close` reports a closed handle "rather than
touching a freed cookie". `TestClosed` holds this.

`Buffer` also refuses an empty buffer, because it passes `&buf[0]` to C.
`TestEmptyBuffer` holds this.

### D8. The code is under the MIT license. Decided.

Commit `9a4fca8` (2026-09-05).

`LICENSE` is the MIT license, "Copyright (c) 2015-2026 Kenneth Shaw". The
Windows archives hold libgnurx, which is under LGPL 2.1 (D4). The questions at
the end ask about that, and about the test data that came from another
project.

### D9. A handle is safe to share across goroutines. Decided. Amends D6.

Commit `dbcd068` (2026-09-12).

`Magic` has a mutex, and every method holds it for the whole call. The
comment on `Magic` says that a single handle is safe to share, "at the cost
of serializing lookups", and that a caller who wants parallel lookups gives
each worker its own handle.

`TestConcurrent` calls one handle from 16 goroutines. The workflow runs the
tests with `-race`, and its comment says that this is "what backs the 'safe
to share a handle' claim".

### D10. A lookup can use other flags and leave the flags of the handle alone. Decided.

Commit `dbcd068` (2026-09-12).

`BufferWith` and `FileWith` identify with the flags that they get.
`BufferTypes` and `FileTypes` identify once for each flag set, under one
lock, so that every result describes the same input. `SetFlags` changes the
flags of the handle. The others do not. `TestFlagsRestored` holds this.

The handle remembers the flags that are on the cookie now, in `cur`, and
calls `magic_setflags` only when they change. The comment in `each` says that
this is "Purely an optimization".

### D11. The tests expect what libmagic 5.48 reports, and file(1) is the reference. Decided.

Commit `dbcd068` (2026-09-12). Commit `a629036` (2026-09-13) added the note
about new file releases to the script.

Each case in `mimeTests` expects the MIME type that libmagic 5.48 reports.
The comment on `mimeTest` says that some values are less specific than the
real type, and calls that "the library's behaviour, not a gap in the test".

`TestOracle` writes each case to a file and compares the result of the
package with `file --mime-type` of the system. It runs only when `file` of
the same version is on `PATH`. Its comment gives the reason: the expected
values alone prove only that the wrapper agrees with itself. The script says
that a new release of file changes the output, "so the Go tests that assert
exact descriptions may need regenerating afterwards".

Found on 2026-09-27: the names of the cases and 16 of the 21 files in
`testdata/` come from the tests of `github.com/gabriel-vasile/mimetype`. The
16 files are byte for byte the same as those in mimetype v1.4.15. The case
names "json issue#239" and "jar in zip #639" name issues of mimetype, not of
this repository. Nothing in this repository says so.

### D12. CI builds and tests only on hosted runners. Decided.

Commit `dbcd068` (2026-09-12) added `.github/workflows/test.yml`. Commit
`dedf132` (2026-09-12) removed two parts of it.

The workflow runs on a push to `main`, on a pull request and by hand. Each of
five hosted runners builds, runs `go vet`, runs `go test -race -count=1 -v
./...` and runs `cmd/file` on three files in `testdata/`. The runners are
linux/amd64, linux/arm64, macOS amd64 (`macos-13`), macOS arm64 and
windows/amd64.

`dedf132` removed the `gofmt` step and the `cross` job. The `cross` job linked
every target with `zig cc`, which was the only CI build of linux/armv7.
Reason not recorded. The commit message is "More cleanup + fixes for
workflows".

### D13. The script builds against the newest stable release of each dependency. Decided.

Commit `a629036` (2026-09-13).

On each run, `build-libmagic.sh` looks up the newest release of zlib, bzip2,
xz, zstd and file. A variable such as `FILE_VER` pins one version. `PINNED=1`
turns off every lookup. If a lookup fails, the script prints a warning and
uses the fallback version. The script ignores a pre-release tag, because the
archives are committed (D1). libgnurx stays at 2.5.1, because upstream
shipped its last release in 2007.

The comment on the fallback versions says that they are what the committed
archives were built from. The same commit rebuilt the archives, and every
`liblzma.a` changed. The archives now hold xz 5.8.4, and `XZ_FALLBACK` still
says 5.8.3. `docs/BACKLOG.md` holds that item.

### D14. The module has no Go dependencies. Proposed.

`go.mod` has no `require` line, and no commit since `7b14d3f` added one. The
package imports only the standard library and `C`. No commit, comment or
document says that this is a rule. Ken, is it one?

### D15. magic is set up for coding agents as every xo repository is. Decided.

Ken decided on 2026-09-27 that every repository in the `xo` namespace is set
up for coding agents the same way. dbmeta records it as dbmeta D110. This
decision adopts it for `magic`.

The repository now has these files:

- `AGENTS.md` holds the rules for a coding agent, and opens with the three
  standing rules of dbmeta D110.
- `CLAUDE.md` holds one line, `@AGENTS.md`, so that Claude Code reads the
  same rules. It is a file and not a symbolic link.
- `README.md` and `CONTRIBUTING.md` are new. `CONTRIBUTING.md` has the
  commands that install the agent skills.
- `skills-lock.json` names the two skills, `simple-english` and
  `go-pedantry`. Each is an ordinary folder in `.agents/skills/` and in
  `.claude/skills/`, installed with `--copy`.
- `.gitignore` ignores `.claude/settings.local.json`, the Claude Code
  permissions of one person.
- `.gitattributes` holds `* text=auto eol=lf`.
- `docs/PLAN.md` is this document, and `docs/BACKLOG.md` holds the known
  work.

`magic` is a small library, so its decisions stay in this document and are
not split into one file each (dbmeta D111).

The tests in `skills_test.go` and `docs_test.go` hold the setup:
`TestSkillsAreCopies`, `TestClaudeImportsAgents`,
`TestTheRootHoldsFourDocuments`, `TestTheDecisionIndexIsComplete`,
`TestAnAmendmentPointsBothWays` and `TestEveryDecisionReferenceExists`. They
use only the standard library.

## Open questions for Ken

1. Is D14 a rule? If it is, a test can read `go.mod` and fail on a `require`
   line, as dburl does.
2. Is linux/armv7 still a supported target? `magic.go` declares it and the
   script builds it, but no CI job built it after `dedf132` removed the
   `cross` job (D12). Do you want the `cross` job back?
3. The `macos-13` job never gets a runner, so every CI run since `dbcd068`
   ends as cancelled after 24 hours. Do you want to move macOS amd64 to
   another runner, or remove it?
4. `dedf132` removed the `gofmt` step from CI. Do you want it back?
5. The test data and the case names came from gabriel-vasile/mimetype, which
   is under the MIT license (D11). Do you want the repository to carry the
   copyright notice of mimetype, and to name mimetype in the two case names
   that cite its issues?
6. The Windows archives link libgnurx statically, and libgnurx is under LGPL
   2.1 (D4). Do you want the README to tell a user of the Windows build about
   that license?
7. The repository has no `.golangci.yml`, and CI runs no linter. Do you want
   one?
