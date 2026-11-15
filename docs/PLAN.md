# Plan

This document holds the plan for porting libmagic to pure Go (D22), the
reviews of the plan, and the open questions for the maintainers. The
decisions are in [`decisions/`](decisions/README.md), one file each (D23).
[`UPSTREAM.md`](UPSTREAM.md) holds the rules for following upstream, and
[`API.md`](API.md) holds the design of the new API.

The plan and the rules came first (D24). Phase 0 ended when the questions of
the port were answered or deferred, and the code of the port started in
phase 1.

## Status

On 2026-11-15 the port is done: phases 0 to 5 have their code, and their
gates hold on the build host. The last release, `v0.4.1`, is the cgo package,
and the port is the next release (D47).

1. Phase 0, the plan, ended when the maintainers answered the questions of
   the port (D33 to D39) and set the API (D40, D42).
2. Phase 1 built the harness (D30). The oracle gives each of the 88 expected
   results of the tests of file. The harness also found that the cgo package
   crashed when it was linked with glibc, and D43 fixed it in `v0.4.1`.
3. Phase 2 built the compiler of the rules and the engine. `internal/rules`
   gives the same 24,985 entries in 4,213 rules as the `magic.mgc` of the
   oracle, byte for byte (D35), and the engine gave the MIME types of the
   oracle.
4. Phase 3 gave every output: the `printf` formats and dates of C, every
   detector, and compressed data, whose decoders a program chooses with a
   blank import (D45). Regular expressions go through a translator for Go's
   `regexp` (D44).
5. Phase 4, the switch, replaced the cgo package with the API of
   `docs/API.md`, and removed the archives and the modules of the targets
   (D47). The license of file is carried as D29 says.
6. Phase 5 followed upstream to its newest commit, `bbffb78d` (D48). Of the
   84 pending commits of the ledger, 55 change what the port does, in the
   rules, the tests or the source, and 29 do not apply. The oracle, the
   table and the gates follow the commit that the ledger names.

The gates on the build host, with cgo off, at `bbffb78d`:

1. `TestPortMatchesTheOracle` asks the engine and the oracle 36,624
   questions: 14 outputs, from the description to the MIME type and the flags
   of compression, for each of 2,471 inputs, as a buffer and as a file. Each
   answer is the same, except 50 where a decoder of Go reads damaged
   compressed data in another way than the library of C. The test lists
   them.
2. `TestAPIMatchesTheOracle` checks each form of a `Result` of the API
   against the oracle, on the same inputs: 13,080 questions (D46).
3. `TestPortPassesTheTestsOfFile` runs the 113 tests of file at that commit,
   and all 113 pass (D37).
4. `TestPortSize` builds a program that prints the MIME type of a file with
   the API. magic adds 978,944 bytes to it, stripped, where the cgo package
   added 995,488 (D38).

Checks outside the gates found no difference either. 50,395 files of the
build host, with 6 output flags each, gave 302,370 answers that are the same
as the oracle. The work that is left is in `docs/BACKLOG.md`.

The tags `v0.1.0` (`7b14d3f`), `v0.2.0` (`dedf132`) and `v0.2.1` (`a629036`)
held the archives in the root module. `v0.3.0` gave each target a module
(D17), `v0.3.1` rebuilt the archives with zlib 1.4 (D13) and broke Windows and
macOS, and `v0.4.0` fixed both and compressed the database (D20, D21).

## Purpose

magic identifies the type of a file or a buffer, as the `file` command does.
The port makes it pure Go: a program that imports it needs the Go toolchain
and nothing else, and builds with `CGO_ENABLED=0` for every target that Go
supports (D22). Before the switch, a program needed a C compiler, and built
for eight targets only.

The output is the output of libmagic built against glibc (D33), at the
commit of file that the ledger names (D48). The port
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
| The tests in `tests/` | 88 test files and 100 expected results at the tag. The release tarball has 83 and 93 |

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

The plan named a package for each part. The code put the parts that share
the state of a detection in one package, `internal/engine`, because libmagic
shares that state between them (D34). The packages, as built:

| Package | What it holds |
| --- | --- |
| `magic`, the root | the public API of `docs/API.md` (D40, D42, D46) |
| `internal/rules` | the compiler of the rules, which gives the entries of `magic.mgc`, and the form of the table (D35) |
| `internal/table` | the compiled rules, embedded, and their generator, which `go generate` runs (D35) |
| `internal/engine` | what `softmagic.c` does, the `printf` formats and dates of C, the translator of regular expressions (D44), and every detector: text, tar, JSON, CSV, SIMH, DER, Compound Document, ELF, the kinds of file, and compressed data |
| `compress` and the packages under it | the registry of the decoders of compressed data, and one decoder for each library of libmagic (D45) |

The port does not read the binary `.mgc` format. It is the memory layout of
the C structs of one release, and it ties the port to that release. The
compiler gives the same entries, and the table stores them in a form of its
own, which is smaller (D38). `MagicDB` went with the switch (D47).

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
   end of each match. D44 records how it was done and measured.
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

D27 gives each compression library a policy. gzip, zlib and bzip2 moved to
the standard library, and xz and zstd come from outside modules (D28). A
program chooses the decoders that it includes with a blank import (D45), so
a program that does not look inside compressed data pays for none.

## Testing plan

All three models say that "100% coverage" needs a definition, and that the
coverage of lines does not prove the same output. The maintainers deferred it
(D37): the aim is to pass the great majority of the tests of file, and to
judge the rest when the port is close. The plan has these gates:

1. The count of the tests of file that pass goes up and never down, and
   each one that fails has a reason (D37). All tests of this
   repository pass. All 113 pass at the commit of D48.
2. The differential test of D30: the oracle and the port read the same bytes
   with the same flags, and any difference fails.
3. A corpus of real files, cut and damaged copies of them, and the files that
   the maintainers approve, through the differential test. The 88 test files
   of file are a gate, and they are far too few to be the whole check.
4. A test for each feature of the rule format, made for the purpose, so that
   every type, relation and modifier runs at least once.
5. Go fuzzing of the reader of the rules, the engine and each parser, for
   panics and for limits on memory and time.
6. The coverage of statements, reported for each package, and not a gate.
7. The size that the port adds to a program, measured against D38.

Gates 1, 2, 3 and 7 are tests in the test module. Items 4, 5 and 6 are not
done, and `docs/BACKLOG.md` holds them.

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
(D26), and the ledger has one line for each commit after it. Phase 5 ported
or set aside each of the 90 commits, and `matched` names the newest (D48).

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
cause is the layout of `regmatch_t` (D43), and none of their
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

The numbers do not repeat. Question 1 was answered by D28, question 9 by D43,
and questions 10 to 16 by D33 to D39. `docs/API.md` has the API, which D42
settled. Questions 2 and 6 were about the archives, which D47 removed: the
`cross` job of CI now builds linux/arm with the other targets that have no
runner, and no archive holds libgnurx. Question 3 was answered by D53,
questions 4, 5, 7 and 8 by D54 to D57, and questions 17 to 22, about the
binary walker, by D49 to D52, and question 23 by D60. No question is open.
The next question is question 24.
