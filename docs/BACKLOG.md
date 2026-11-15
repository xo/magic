# Backlog

This document holds the work that is known and not done. Each item names
where it came from.

On 2026-09-27, the repository had no open issue, no pull request, open or
closed, and no `TODO` or `FIXME` comment. Every item below came from the CI
history, from the code or from the work that set up D15.

When you finish an item, remove it. If an item needs a decision, it becomes
an open question at the end of `docs/PLAN.md`, and the answer becomes a file
in `docs/decisions/`.

The cgo package and its archives left with the switch (D47), and with them
the items about the archives, the build script and the handle. The binary
walker comes after phase 5 (D31).

## CI

1. The `macos-13` job never gets a runner, and GitHub cancels it after 24
   hours. On 2026-11-15 the `macos-14` job was cancelled before it started
   too, so no macOS target has CI. Source: runs 34652233745, 34732334420 and
   43657823492 of the `test` workflow. Open question 3 in `docs/PLAN.md`.
2. CI does not run `gofmt`. `dedf132` removed the step. Source: D12. Open
   question 4 in `docs/PLAN.md`.
3. The repository has no `.golangci.yml`, and CI runs no linter. Source:
   found while this setup was written. Open question 7 in `docs/PLAN.md`.
4. The `cross` job builds the targets that have no runner, such as
   freebsd/amd64 and linux/arm, and runs nothing on them. Source: D47.
5. A rule for CI before a tag is not decided. resvg tagged before CI finished.
   For `v0.3.0`, the tags waited for CI. Source: open question 8 in
   `docs/PLAN.md`.

## The package

6. The text of an error of the engine is the text of libmagic, which is not
   in the gerund form that `go-pedantry` asks for. The maintainers have not
   decided which one wins. Source: the switch, D47.
7. Add a test that reads the `go.mod` files and fails on a `require` line for
   a module that the maintainers did not approve, as dburl does. D28 and D45
   approve `github.com/ulikunitz/xz` and `github.com/klauspost/compress` for
   the decoder modules only. Source: D14, D28 and D45.
8. A `Result` comes from one walk of the engine for each of its forms (D46).
   One walk that records every form would be about four times faster, and
   must give the same answers to `TestAPIMatchesTheOracle`.
9. `DetectFile` leaves out the extension and the Apple code of a file that is
   not regular. libmagic, asked for them, reads a link as the file it points
   to, and fails on a directory. Source: `ExampleDetectFile`, D47.

## The tests

10. The case names and 16 of the 21 files in `testdata/` came from the tests
    of `github.com/gabriel-vasile/mimetype`, which is under the MIT license.
    The repository does not carry its copyright notice. Two case names, "json
    issue#239" and "jar in zip #639", cite issues of mimetype and do not name
    it. Source: `magic_test.go` and `testdata/`, compared with mimetype
    v1.4.15. Open question 5 in `docs/PLAN.md`.
11. Some cases in `mimeTests` repeat. "kml 2.2" and "ogg" are each the
    name of two cases, so `go test` reports the second one as `ogg#01`.
    "rpm" has the same input as "rpm 1". "svg with comment prefix" has the
    same input as "svg no xml header" and holds no comment. Source:
    `magic_test.go`.
12. `TestOracle` compares MIME types only with the `file` on `PATH`. The test
    module compares every output with the oracle, so this is a quick check,
    and not the gate.

## The port

Phases 2 to 4 found these. None of them changes an answer of the gates.

13. The decoders of Go do not name each fault of damaged compressed data as
    the libraries of C do. zlib prints "invalid distance too far back" where
    the port prints "data error", and libzstd and liblzma give other codes
    for some damaged frames. `TestPortMatchesTheOracle` lists these answers
    apart. A port of the error checks of inflate would close the zlib part.
    Source: `inftrees.c` and `inflate.c` of zlib.
14. The reason that libmagic prints after "Composite Document File V2
    Document" can come from `errno` of an earlier step, because a stream of
    length 0 sets no `errno`. The port does not copy this. Source:
    `cdf_read_long_sector_chain` and `file_trycdf`.
15. The oracle keeps one cookie for all questions. libmagic keeps the value
    of the last rule and the levels of continuation in the cookie, and a
    string compare reads past the end of a string. So an answer of libmagic
    can depend on the question before it. The port starts each detection
    from zero. No test has found a difference. Source: `ms_value` and `c.li`
    in `file.h`.
16. The decoded table holds every entry as a struct of 432 bytes, so a
    program holds about 10.8 MB of rules in memory, as libmagic does. A
    smaller form in memory would cost a change of the engine.
17. The xz decoder gives the data of the first block of a cut stream, where
    liblzma gives the data of every block that it reached.
18. With `MAGIC_NO_COMPRESS_FORK`, file 5.48 refuses its libraries and still
    runs outside programs. The port copies the first part and never runs a
    program (D36, D45).

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
