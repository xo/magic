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

The tags `v0.1.0` (`7b14d3f`), `v0.2.0` (`dedf132`) and `v0.2.1` (`a629036`)
hold the archives in the root module. No document says what rule chose their
numbers. Ken chose `v0.3.0` on 2026-10-14 for the first release with a module
for each target (D17). It
wraps libmagic 5.48 from file 5.48, and links it with zlib 1.3.2, bzip2 1.0.8,
xz 5.8.4 and zstd 1.5.7. The Windows archives also hold libgnurx 2.5.1.

The package is `magic.go`, with one generated file `link_<goos>_<goarch>.go`
for each of the eight targets (D17, D19). `magic.go` has these parts:

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
on six hosted runners, and freebsd/amd64 has no job (D19). Five of the six
passed on the last run, including windows/arm64 (D16). The macOS amd64 job
never starts, so every run ends as cancelled. `docs/BACKLOG.md` holds that item and the other
known work.

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
| [D1](#d1-the-package-links-static-libmagic-archives-that-the-repository-carries-decided-amended-by-d16-amended-by-d17-amended-by-d19) | The package links static libmagic archives that the repository carries | Decided. Amended by D16. Amended by D17. Amended by D19 |
| [D2](#d2-build-libmagicsh-cross-compiles-the-archives-with-zig-cc-decided-amended-by-d16-amended-by-d17) | build-libmagic.sh cross-compiles the archives with zig cc | Decided. Amended by D16. Amended by D17 |
| [D3](#d3-the-magic-database-is-embedded-and-one-copy-serves-every-target-decided) | The magic database is embedded, and one copy serves every target | Decided |
| [D4](#d4-libmagic-reads-compressed-files-through-zlib-bzip2-xz-and-zstd-decided) | libmagic reads compressed files through zlib, bzip2, xz and zstd | Decided |
| [D5](#d5-the-api-follows-the-libmagic-c-api-decided) | The API follows the libmagic C API | Decided |
| [D6](#d6-a-handle-is-not-safe-for-concurrent-use-decided-amended-by-d9) | A handle is not safe for concurrent use | Decided. Amended by D9 |
| [D7](#d7-a-closed-handle-reports-an-error-and-never-reaches-c-decided) | A closed handle reports an error and never reaches C | Decided |
| [D8](#d8-the-code-is-under-the-mit-license-decided) | The code is under the MIT license | Decided |
| [D9](#d9-a-handle-is-safe-to-share-across-goroutines-decided-amends-d6) | A handle is safe to share across goroutines | Decided. Amends D6 |
| [D10](#d10-a-lookup-can-use-other-flags-and-leave-the-flags-of-the-handle-alone-decided) | A lookup can use other flags and leave the flags of the handle alone | Decided |
| [D11](#d11-the-tests-expect-what-libmagic-548-reports-and-file1-is-the-reference-decided) | The tests expect what libmagic 5.48 reports, and file(1) is the reference | Decided |
| [D12](#d12-ci-builds-and-tests-only-on-hosted-runners-decided-amended-by-d16) | CI builds and tests only on hosted runners | Decided. Amended by D16 |
| [D13](#d13-the-script-builds-against-the-newest-stable-release-of-each-dependency-decided) | The script builds against the newest stable release of each dependency | Decided |
| [D14](#d14-the-module-has-no-go-dependencies-proposed) | The module has no Go dependencies | Proposed |
| [D15](#d15-magic-is-set-up-for-coding-agents-as-every-xo-repository-is-decided) | magic is set up for coding agents as every xo repository is | Decided |
| [D16](#d16-windowsarm64-is-a-target-decided-amends-d1-amends-d2-amends-d12-amended-by-d17) | windows/arm64 is a target | Decided. Amends D1. Amends D2. Amends D12. Amended by D17 |
| [D17](#d17-each-target-is-a-go-module-of-its-own-tagged-as-resvg-tags-its-modules-decided-amends-d1-amends-d2-amends-d16) | Each target is a Go module of its own, tagged as resvg tags its modules | Decided. Amends D1. Amends D2. Amends D16 |
| [D18](#d18-a-target-version-has-a-patch-number-and-a-target-module-declares-go-121-decided) | A target version has a patch number, and a target module declares go 1.21 | Decided |
| [D19](#d19-magic-builds-the-targets-that-resvg-builds-decided-amends-d1) | magic builds the targets that resvg builds | Decided. Amends D1 |

### D1. The package links static libmagic archives that the repository carries. Decided. Amended by D16. Amended by D17. Amended by D19.

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

### D2. build-libmagic.sh cross-compiles the archives with zig cc. Decided. Amended by D16. Amended by D17.

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

### D12. CI builds and tests only on hosted runners. Decided. Amended by D16.

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
said 5.8.3. On 2026-10-14, Ken asked for a check of every dependency. Each
was at its newest stable release, and `XZ_FALLBACK` was set to 5.8.4.

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

### D16. windows/arm64 is a target. Decided. Amends D1. Amends D2. Amends D12. Amended by D17.

Ken decided on 2026-10-14 to add windows/arm64, after he asked Gemini Pro and
DeepSeek Pro which platforms to add. Gemini Pro recommended it, because
Windows on arm64 hardware is growing and GitHub has a hosted runner for it.
DeepSeek Pro put it on request and said that no hosted runner exists. The
label list of `actionlint` holds `windows-11-arm`, so that claim was wrong.

What changed:

- `libmagic/windows-arm64/` holds the same six archives as `windows-amd64`,
  8.2 MB in all. `build-libmagic.sh` built them with `zig cc` for
  `aarch64-windows-gnu`. All six are `coff-arm64`.
- `magic.go` has a `#cgo` line for windows/arm64.
- The `test` workflow has a `windows/arm64` job on `windows-11-arm`. That
  runner has no arm64 mingw-w64, so the job uses `zig cc` as the C compiler.
  It does not pass `-race`, because Go does not support the race detector on
  windows/arm64.

Two things differ from the other targets in the script, and neither changes
an archive of another target:

- A configure script with only `--host` runs a test program to find out
  whether it is cross-compiling. On this host, wine tries to run the arm64
  program and hangs. The `noexec` field in `TARGETS` makes the script pass
  `--build` as well, so that configure runs nothing.
- xz builds a version resource for Windows, and its configure looks for
  `<host>-windres`. The host has `windres` only for x86_64 and i686, so the
  script links `llvm-windres` under the name of the host and gives it the
  mingw headers of zig. The resource is in `liblzma.a` for both Windows
  targets.

The same change copies each source tree with `cp -rp`. Without `-p`, the
copy gets new timestamps, and `make` then tried to regenerate `aclocal.m4`
in xz and failed.

Test status: the build host links a `file.exe` and a test binary for
windows/arm64 with `zig cc`. It cannot run them, because it has no arm64
Windows. The first run of the CI job is the first time that the tests run on
windows/arm64.

The first run failed, because zig for arm64 Windows crashed on the runner
with exit 0xc0000005 while it built `runtime/cgo`. The job now installs
llvm-mingw 20261006 and uses its clang as the C compiler. On that run, every
test passed on windows/arm64, and `TestOracle` passed. So the archives that
zig built link and work under a second compiler.

Ken also asked magic to match the targets of resvg. The list of resvg is not
final, and open question 8 holds what is left.

### D17. Each target is a Go module of its own, tagged as resvg tags its modules. Decided. Amends D1. Amends D2. Amends D16.

Ken decided on 2026-10-14 that magic follows the tagging pattern of resvg. That
pattern is in D9 of resvg and in `libresvg/README.md` of resvg. One module for
all targets made every user download every archive, and each new target made it
worse.

What changed:

- Each folder `libmagic/<goos>_<goarch>/` is a module with its own `go.mod`,
  its archives and a generated `link.go` that holds the `#cgo` line. The name
  `linux_arm` is armv7. The new names replace `linux-armv7`, `macos-amd64` and
  `macos-arm64`.
- `magic.go` has no `#cgo LDFLAGS` line for a platform any more. A file
  `link_<goos>_<goarch>.go` in the root imports the module of its platform.
  Go reads the end of the file name as a build constraint. The script writes
  both files.
- The root `go.mod` requires each target module and has a `replace` line that
  points it at its folder. The replace lines make the repository build before
  the tags exist. A user of the root module ignores them.
- A folder that has a `go.mod` is not part of the module above it. The root
  module no longer holds the archives, so a user downloads the archives of one
  platform and no other.
- `magic.h` and `magic.mgc` stay in `libmagic/` in the root module, because
  they are the same for every target.

The tags follow resvg, with one difference that Go forces:

- The root tag is a plain semver, with no prefix and no upstream version. It
  tracks the API of the package. A change to the wrapper alone bumps the root.
  A new target bumps the root minor. The root tags that exist are `v0.1.0`,
  `v0.2.0` and `v0.2.1`.
- A target tag is the path of the folder and the version of file that the
  archives hold, such as `libmagic/linux_amd64/v5.48.0`. A new release of file
  means new tags for every target, then an edit of the `require` lines, then a
  new root tag.
- The module path ends in `/v5`, such as
  `github.com/xo/magic/libmagic/linux_amd64/v5`. Go wants `/vN` on a module at
  version 2 or higher. resvg has no suffix, because its versions start with
  `v0`. The folder and the tags do not change.
- The order of a release is: push the commit, tag and push the target modules,
  edit the `require` lines if a version changed, then tag and push the root.
  A published tag never moves, because the module proxy caches it.

`TestEveryTargetIsWired` and `TestNoLinkFileWithoutATarget` hold the layout.

Nothing is tagged by this decision, because Ken did not say to tag. The tests
pass with the replace lines. No one has run `go get` from an empty module
against real tags, because the tags do not exist. `docs/BACKLOG.md` holds that
item.

### D18. A target version has a patch number, and a target module declares go 1.21. Decided.

These two details follow resvg and a limit of Go. Ken accepted them on
2026-10-14.

A version of file has two parts, such as 5.48, and a Go version has three. The
script adds `.0`, so the tag is `v5.48.0`. A published tag never moves. If a
rebuild keeps the same version of file, for example for a new xz, the tag
needs the next patch number, `v5.48.1`. The script prints `.0` and does not
know about a patch.

The `go.mod` of a target module says `go 1.21`, as the platform modules of
resvg do. The root says `go 1.25`. Nothing in a target module needs more than
1.21.


### D19. magic builds the targets that resvg builds. Decided. Amends D1.

Ken decided on 2026-10-14 that magic covers only the targets of resvg, and that
it needs no linux/ppc64le and no linux/s390x. The targets are linux/amd64,
linux/arm64, linux/arm, darwin/amd64, darwin/arm64, windows/amd64,
windows/arm64 (D16) and freebsd/amd64.

freebsd/amd64 is new. The script builds it with `zig cc` for `x86_64-freebsd`
and the `noexec` field of D16. It has no CI job, because GitHub has no hosted
FreeBSD runner.

resvg also has `linux_amd64_musl` and `linux_arm64_musl`, which a user selects
with `-tags musl`. magic does not add them. Its Linux archives are built for
musl and are static, so a program links them under glibc and under musl. Ken
accepted this on 2026-10-14. On linux/amd64, the tests passed when the program
was linked with the glibc gcc of the host and with `zig cc` for musl. On
linux/arm and linux/arm64, they passed linked with `zig cc` for musl and run
under qemu. A glibc link of arm and arm64 was not tried.

Archives for linux/riscv64, linux/ppc64le and linux/s390x were built on
2026-10-14 and deleted, and none was committed.

What ran on 2026-10-14:

- The tests passed on linux/amd64, linux/arm and linux/arm64, and on
  windows/amd64 under wine.
- The program and the test binary linked, and nothing ran them here, for
  windows/arm64 and freebsd/amd64. After the commit, windows/arm64 ran in
  CI and passed, as D16 says. freebsd/amd64 has not run.
- The package compiled, and was not linked, for darwin/amd64 and
  darwin/arm64. `zig` has no macOS SDK, so the CI jobs on macOS do the link.

The targets of resvg are not final. resvg staged windows/arm64 and
freebsd/amd64 and did not commit them. If resvg drops one, Ken decides whether
magic does too.

## Open questions for Ken

1. Is D14 a rule? If it is, a test can read `go.mod` and fail on a `require`
   line, as dburl does.
2. Is linux/armv7 still a supported target? `magic.go` declares it and the
   script builds it, but no CI job built it after `dedf132` removed the
   `cross` job (D12). Do you want the `cross` job back?
3. The `macos-13` job never gets a runner, so every CI run since `dbcd068`
   ends as cancelled after 24 hours. `actionlint` lists `macos-15-intel` as a
   current Intel runner. Do you want to move macOS amd64 to it, or remove
   it?
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
8. resvg tagged its modules before CI finished on the commit, and says that
   no rule requires CI to pass first. Do you want a rule that CI passes before
   any tag, for magic? CI cannot run freebsd/amd64, so that target would stay
   untested.
