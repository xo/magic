# Plan

This document holds the plan for porting libmagic to pure Go (D22), the
reviews of the plan, and the open questions for the maintainers. The
decisions are in [`decisions/`](decisions/README.md), one file each (D23).
[`UPSTREAM.md`](UPSTREAM.md) holds the rules for following upstream, and
[`API.md`](API.md) holds the design of the new API.

No code of the port exists. The plan and the rules come first (D24), and the
plan is ready when its open questions are answered or deferred.

## Status

On 2026-11-15 magic is a cgo wrapper of libmagic, at `v0.4.0`:

1. It wraps libmagic 5.48 from file 5.48, and links it with zlib 1.4, bzip2
   1.0.8, xz 5.8.4 and zstd 1.5.7. The Windows archives also hold libgnurx
   2.5.1.
2. It has static archives for eight targets: linux/amd64, linux/arm64,
   linux/arm (armv7), darwin/amd64, darwin/arm64, windows/amd64, windows/arm64
   and freebsd/amd64 (D19). Each target is a Go module of its own (D17).
3. It embeds the database compressed with zstd, and expands it into C memory
   (D20). `MagicDB` returns the plain database (D21).
4. CI tests linux/amd64, linux/arm64, windows/amd64 and windows/arm64. macOS
   has no CI, and freebsd/amd64 has no runner.

The port has not started. Phase 0, this plan, is open. On 2026-11-15 the
maintainers answered questions 10 to 16 (D33 to D39), set the new API and its
entry `magic.Detect` (D40), and moved the module to Go 1.27 (D41).

The tags `v0.1.0` (`7b14d3f`), `v0.2.0` (`dedf132`) and `v0.2.1` (`a629036`)
held the archives in the root module. `v0.3.0` gave each target a module
(D17), `v0.3.1` rebuilt the archives with zlib 1.4 (D13) and broke Windows and
macOS, and `v0.4.0` fixed both and compressed the database (D20, D21).

## Purpose

magic identifies the type of a file or a buffer, as the `file` command does.
The port makes it pure Go: a program that imports it needs the Go toolchain
and nothing else, and builds with `CGO_ENABLED=0` for every target that Go
supports (D22). Today a program needs a C compiler, and builds for eight
targets only.

The output is the output of libmagic 5.48 built against glibc (D33). The port
copies what libmagic does, not how its C code is written (D34). The API is new
and need not stay compatible (D39, D40). A program that uses the port grows by
no more bytes than it grows with the cgo package (D38).

## What upstream holds

The numbers are for file 5.48, the base commit (D26), measured on 2026-11-15.

| Part | Size |
| --- | --- |
| libmagic, the C library in `src/` | 48 files, 23,251 lines. `file.c` and `seccomp.c` are not ported (D27) |
| The rule engine | `apprentice.c` 3,726 lines reads the rules, `softmagic.c` 2,604 lines matches them |
| Format parsers in C | ELF 1,980 lines, Compound File (OLE2) 2,384, compression 1,302, text encoding 1,059, JSON 500, DER 458, CSV, tar and SIMH 594 |
| The rules in `magic/Magdir` | 356 files, 1.9 MB, 28,161 lines that are not comments |
| Rules that use a regular expression | 368, and none uses a backreference |
| Rules that use `search` | 780 |
| Rules that print a date | 102 |
| Rules that print a float with `%g`, `%f` or `%e` | 31 |
| Lines that use `indirect` | 65 |
| Lines that use `name` or `use`, which are subroutines | 2,149 |
| The tests in `tests/` | 83 input files and 93 expected results |

Upstream has 90 commits after the base commit (D26). At its newest commit the
tests have 113 input files and 125 expected results. The ledger in
`docs/upstream/file.tsv` holds each commit.

## Scope

The port covers:

1. libmagic: the reader of the rules, the engine, the output and every
   detector that is C code, such as ELF, Compound File, JSON, CSV, tar, DER,
   SIMH and the text encodings.
2. The rules, compiled into tables when the package is built (D35).
3. What the flags of libmagic do, as the options of the new API (D40,
   `docs/API.md`).
4. The tests of file, and the tests of this repository (D37).

The port does not cover:

1. The command line program of file. `cmd/file` stays a small example (D27).
2. The binary walker, which comes after the port (D31). `docs/BACKLOG.md`
   holds what is known about it.
3. Outside programs that libmagic runs for some formats. magic runs none
   (D36).

## Architecture

The three models of "The reviews" agree on the shape. The names are a
proposal, and the first file of each package settles them. The packages are
designed as Go, not as copies of the files of libmagic (D34).

| Package | What it holds |
| --- | --- |
| `magic`, the root | the public API of `docs/API.md` (D40) |
| `internal/rules` | the compiler of the rules, run by `go generate`, and the tables that it writes (D35) |
| `internal/engine` | what `softmagic.c` does: offsets, indirect offsets, `name` and `use`, the order of strength, `default` and `clear`, and continuation |
| `internal/cfmt` | output that matches C: `printf` formats, dates and floats |
| `internal/elf`, `internal/cdf`, `internal/text`, `internal/compress` and the other detectors | the parsers that are C code in libmagic |

The rules are compiled into tables when the package is built (D35). The port
does not read the binary `.mgc` format. It is the memory layout of the C
structs of one release, and it ties the port to that release.

The tables are most of the bytes that the port adds to a program, so their form
is chosen by what they cost (D38). `MagicDB` goes, because a Go engine has no
`.mgc` to give.

## The hard parts

The three models name the same risks. These are in order of risk.

1. Regular expressions. libmagic compiles and runs each one in the C locale,
   in `file_regcomp` and `file_regexec` in `funcs.c`, so a match works on
   bytes. Go's `regexp` works on UTF-8, and `.` matches a whole multi-byte
   character where C matches one byte. dburl D3 found the same fault. One way
   is to read each byte as the character with the same number, 0 to 255, and
   to translate each pattern to match. No rule uses a backreference, so
   `regexp.CompilePOSIX` can express each pattern. Whether it gives the same
   match as the C library needs a test of all 368 rules, on the start and the
   end of each match.
2. Output. C `printf` differs from Go `fmt` for `%g`, exponents, rounding,
   `%#o` and widths. The port needs a small formatter that copies C, tested
   against C on the 31 float rules and on each integer format.
3. Dates. 102 rules print a date, in local time or in UTC, with different
   epochs. The tests pin `TZ`.
4. The order of results. The strength of each rule decides which result comes
   first. The sort and its ties must match, or the first line differs.
5. Text and encodings. `ascmagic.c` and `encoding.c` decide "ASCII text",
   "UTF-8 text" and the like with rules of thumb. They need a port, not a new
   design.
6. Size. The cgo package adds 995,488 bytes to a stripped program (D38), and
   the port must add no more. The tables, a decoder for xz or zstd and the
   generic entry each count. A test measures it from the first code on.
7. ELF and Compound File. These are full parsers for hostile input. Go has
   `debug/elf`. libmagic describes a damaged file as far as it can read it,
   and whether `debug/elf` is as tolerant is not tested. If it is not, the
   port needs a parser of its own.

## Compression

D27 gives each compression library a policy. gzip, zlib and bzip2 move to the
standard library. xz and zstd come from outside packages when they are needed
(D28), because the standard library has neither. Without them, the port names
a file that is compressed with xz or zstd and does not look inside it, and its
output differs from libmagic with `MAGIC_COMPRESS`.

## Testing plan

All three models say that "100% coverage" needs a definition, and that the
coverage of lines does not prove the same output. The maintainers deferred it
(D37): the aim is to pass the great majority of the tests of file, and to
judge the rest when the port is close. The plan has these gates:

1. The count of the 93 expected results of file that pass goes up and never
   down, and each one that fails has a reason (D37). All tests of this
   repository pass.
2. The differential test of D30: the oracle and the port read the same bytes
   with the same flags, and any difference fails.
3. A corpus of real files, cut and damaged copies of them, and the files that
   the maintainers approve, through the differential test. The 83 test files
   of file are a gate, and they are far too few to be the whole check.
4. A test for each feature of the rule format, made for the purpose, so that
   every type, relation and modifier runs at least once.
5. Go fuzzing of the reader of the rules, the engine and each parser, for
   panics and for limits on memory and time.
6. The coverage of statements, reported for each package, and not a gate.
7. The size that the port adds to a program, measured against D38.

## Estimates

Engineer-weeks for one experienced Go engineer, from the three reviews and
from a reading of the code:

| Source | Port of libmagic | Effect of an AI agent |
| --- | --- | --- |
| Gemini Pro | 12 to 22 | 20 to 30% less |
| DeepSeek Pro | 12 to 18 | 20 to 30% less |
| OpenAI Pro | 30 to 60 | 10 to 25% less |
| This plan | 24 to 47 | 15 to 30% less, and less off the high figure |

The models agree on the parts. They differ on how long it takes to make every
output equal, which OpenAI Pro thinks is most of the cost. The figures of this
plan, by part:

| Part | Weeks |
| --- | --- |
| The oracle, the differential test in CI and the corpus | 3 to 5 |
| The reader of the rules, and the tables | 3 to 5 |
| The engine: offsets, `name` and `use`, strength | 5 to 8 |
| Output that matches C: formats, dates, floats, regular expressions | 3 to 6 |
| The detectors: text, ELF, Compound File, JSON, CSV, tar, DER, SIMH | 5 to 9 |
| Compression | 1 to 2 |
| Making every output equal on the corpus | 4 to 12 |
| Total | 24 to 47 |

The uncertainty is in the work of making every output equal, and it depends on
what the corpus finds. An AI agent writes the first form of parsers, tables
and tests fast. It does less for the time spent finding why one file of ten
thousand differs, and that is where the high figures come from.

## Phases

Each phase ends with a gate. The cgo package stays the one that users get
until phase 4.

0. The plan. The open questions are answered or deferred, and the decisions
   are settled (D24).
1. The harness: the test module, the oracle built from the checkout (D30),
   the differential test, the corpus, and the tests of file as test data.
   Gate: the oracle and the cgo package of today agree on the corpus, on the C
   library that each is built for.
2. The rules and the engine, and enough of the detectors that MIME types
   match. Gate: every MIME type equals the oracle on the corpus. The port can
   ship beside the cgo package, so that users can compare.
3. Full output: the formats of C, every detector and compression. Gate: the
   great majority of the expected results of file pass, the maintainers judge
   the rest (D37), and the size is within D38.
4. The switch. The Go engine and the new API replace the cgo package, and
   the archives, the build script and the modules for each target leave the
   repository (D22). The release is the next minor version (D39). Gate: the
   gates of phase 3, with `CGO_ENABLED=0`, on every target that CI has.
5. Following upstream: the `pending` commits of the ledger, in order (D26).
   Gate: `matched` is the newest commit of upstream.

## Following upstream

[`UPSTREAM.md`](UPSTREAM.md) holds the rules. The base commit is `FILE5_48`
(D26), and the ledger has one line for each commit after it. Before phase 5,
every commit that changes the library, the rules or the tests is `pending`.

## Risks

1. The port does not reach the same output on some files, after the time that
   the estimate gives. The differential test makes the gap visible, so the
   maintainers can decide what to accept.
2. The tests of file are few, so the corpus decides how much of the engine is
   tested. A small corpus gives a false sense of parity.
3. Upstream moves while the port is written. 90 commits came in five months
   after 5.48. The ledger keeps them in order, and phase 5 ports them.
4. An outside package for xz or zstd can change or stop. D28 keeps that
   dependency to two formats.

## Prior art

okage ports tiny-skia and resvg from Rust in the same way, with the same rules
for decisions, checkouts, ledgers and oracles (okage D1, okage D11, okage D12
and okage D22). Its peer session knows how a port that follows upstream works.

## The reviews

On 2026-11-15 three models read the same brief, with the numbers of "What
upstream holds", and answered the same seven questions. The brief told them,
wrongly, that the Netpbm fault came from two regex engines that differ. The
cause is the layout of `regmatch_t` (`docs/BACKLOG.md`), and none of their
advice depends on it.

1. Gemini Pro (`gemini-3.1-pro-preview`): 12 to 22 weeks for the port. Parse
   the rules at `go generate` time, and do not read `.mgc`. Use
   `github.com/ulikunitz/xz` and `github.com/klauspost/compress/zstd`. Aim for
   the same MIME types and accept small differences in the text, which this
   plan does not adopt, because the goal is the same output. It said that the
   rules sometimes use backreferences. They do not: the one hit for `\1` to
   `\9` is `\012`, an octal newline.
2. DeepSeek Pro (`deepseek-v4-pro`): 12 to 18 weeks. Embed the rule text and
   parse it at run time, because that is easier to debug. Pin the reference
   to one C library, the C locale and one time zone.
3. OpenAI Pro (`gpt-6-astra`): 30 to 60 weeks. Compile the rules at
   `go generate` time. Build a C oracle in CI that the package itself does not
   use. "100% coverage of all unit tests" can mean two things, and every test
   passing is the sound gate.

All three:

1. Do not read `.mgc`.
2. Use the two outside packages for xz and zstd, because the standard library
   has neither.
3. Test against C libmagic with a large corpus, because the tests of file are
   too few.
4. Name the reference for "the same output", because there is no single right
   answer without one.

## Open questions

The numbers do not repeat. Question 1 was answered by D28, and questions 10 to
16 by D33 to D39. `docs/API.md` has the API, which D42 settled. The next
question is question 17.

2. Is linux/armv7 still a supported target? `magic.go` declares it and the
   script builds it, but no CI job built it after `dedf132` removed the
   `cross` job (D12). Is the `cross` job wanted back?
3. The `macos-13` job never gets a runner, and on 2026-11-15 the `macos-14`
   job was cancelled before it started, so macOS has no CI. `actionlint`
   lists `macos-15-intel` and `macos-15` as current runners. Do the macOS jobs
   move to them?
4. `dedf132` removed the `gofmt` step from CI. Is it wanted back?
5. The test data and the case names came from gabriel-vasile/mimetype, which
   is under the MIT license (D11). Does the repository carry the copyright
   notice of mimetype, and name mimetype in the two case names that cite its
   issues?
6. The Windows archives link libgnurx statically, and libgnurx is under LGPL
   2.1 (D4). Does the README tell a user of the Windows build about that
   license?
7. The repository has no `.golangci.yml`, and CI runs no linter. Is one
   wanted?
8. resvg tagged its modules before CI finished on the commit, and no rule
   requires CI to pass first. Is there a rule that CI passes before any tag?
   `v0.3.1` was tagged while CI was down, and CI found its Windows fault four
   days later.
9. A program that links the Linux archives with glibc gets wrong descriptions
   (`docs/BACKLOG.md`). The port ends the fault at phase 4. Until then, does
   magic add glibc builds and musl modules, as resvg has, or does it document
   the fault and wait for the port?
